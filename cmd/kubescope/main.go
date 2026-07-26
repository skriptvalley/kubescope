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
	"sync"
	"syscall"
	"time"

	"github.com/skriptvalley/kubescope/internal/config"
	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/server"
	"github.com/skriptvalley/kubescope/internal/stream"
	"github.com/skriptvalley/kubescope/web"
)

// shutdownTimeout bounds the graceful drain after a signal. Overrunning it is
// reported but not fatal: termination was requested, so the process exits 0.
const shutdownTimeout = 10 * time.Second

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

	mgr := kube.NewManager(cfg.KubeconfigSources)

	// Sprint 6: exec sessions and port-forwards are per-context live sessions.
	// They must not outlive their context, so a context switch tears down every
	// session bound to another context, and shutdown tears down all of them.
	execSessions := stream.NewExecRegistry()
	portForwards := stream.NewPortForwardManager(mgr, logger)
	// A runtime kubeconfig swap (ADR-0007) replaces the credential source
	// entirely: every live session built on the old source is torn down. The
	// stream/discovery layers handle themselves via the source generation.
	mgr.SetSourceObserver(func() {
		execSessions.CloseAll()
		portForwards.CloseAll()
	})
	mgr.SetSwitchObserver(func(current string) {
		execSessions.CloseOthers(current)
		portForwards.CloseOthers(current)
	})

	// SSE streams (watch feeds, pod logs) are long-lived HTTP requests that end
	// only when the client goes away, and Shutdown waits for active requests — so
	// without a drain signal a single open browser tab pins the server for the
	// whole shutdown timeout. Closing this channel tells those handlers to return.
	drain := make(chan struct{})
	closeDrain := sync.OnceFunc(func() { close(drain) })

	handler := server.New(server.Options{
		Logger:             logger,
		Drain:              drain,
		Kube:               mgr,
		Stream:             mgr,
		Exec:               mgr,
		ExecSessions:       execSessions,
		PortForwards:       portForwards,
		ReadOnly:           cfg.ReadOnly,
		AuthMode:           cfg.AuthMode,
		BasicAuthUsername:  cfg.BasicAuthUsername,
		BasicAuthPassword:  cfg.BasicAuthPassword,
		AllowKubeconfigSet: cfg.AllowKubeconfigSet,
		ListenAddr:         cfg.ListenAddr,
		Dist:               web.Dist(),
	})

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Fires as soon as Shutdown starts, before it begins waiting on active
	// requests — the streaming handlers see it and unwind while the drain runs.
	httpServer.RegisterOnShutdown(closeDrain)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("kubescope starting",
			"listen_addr", cfg.ListenAddr,
			"kubeconfig_sources", cfg.KubeconfigSources,
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		// Stop accepting first, then tear down live sessions: Shutdown does not
		// drain hijacked WebSockets (exec) or close port-forward listeners, so
		// they are closed explicitly (Sprint 6 cleanup).
		err := httpServer.Shutdown(shutdownCtx)
		execSessions.CloseAll()
		portForwards.CloseAll()
		if err != nil {
			// Everything the process owns is torn down by the lines above, and the
			// operator asked it to stop — a drain that overran the deadline is worth
			// reporting but is not a failed run, so it must not become exit(1).
			logger.Warn("graceful shutdown did not complete before the deadline",
				"error", err, "timeout", shutdownTimeout)
		}
		logger.Info("shutdown complete")
		return nil
	}
}
