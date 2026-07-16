// Command kubescope runs the Kubescope server: one binary serving the API
// and the embedded SPA.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skriptvalley/kubescope/internal/config"
	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/server"
	"github.com/skriptvalley/kubescope/internal/stream"
	"github.com/skriptvalley/kubescope/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("kubescope exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	mgr := kube.NewManager(cfg.KubeconfigPath)

	// Sprint 6: exec sessions and port-forwards are per-context live sessions.
	// They must not outlive their context, so a context switch tears down every
	// session bound to another context, and shutdown tears down all of them.
	execSessions := stream.NewExecRegistry()
	portForwards := stream.NewPortForwardManager(mgr, logger)
	mgr.SetSwitchObserver(func(current string) {
		execSessions.CloseOthers(current)
		portForwards.CloseOthers(current)
	})

	handler := server.New(server.Options{
		Logger:            logger,
		Kube:              mgr,
		Stream:            mgr,
		Exec:              mgr,
		ExecSessions:      execSessions,
		PortForwards:      portForwards,
		ReadOnly:          cfg.ReadOnly,
		AuthMode:          cfg.AuthMode,
		BasicAuthUsername: cfg.BasicAuthUsername,
		BasicAuthPassword: cfg.BasicAuthPassword,
		ListenAddr:        cfg.ListenAddr,
		Dist:              web.Dist(),
	})

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("kubescope starting",
			"listen_addr", cfg.ListenAddr,
			"kubeconfig", cfg.KubeconfigPath,
			"read_only", cfg.ReadOnly,
			"auth_mode", cfg.AuthMode,
		)
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Stop accepting first, then tear down live sessions: Shutdown does not
		// drain hijacked WebSockets (exec) or close port-forward listeners, so
		// they are closed explicitly (Sprint 6 cleanup).
		err := httpServer.Shutdown(shutdownCtx)
		execSessions.CloseAll()
		portForwards.CloseAll()
		if err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}
