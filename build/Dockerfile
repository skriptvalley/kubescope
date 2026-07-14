# syntax=docker/dockerfile:1
# Multi-stage, multi-arch build (ADR-0002): Node builds the SPA, Go embeds it,
# distroless runs it. Build from the repo root:
#   docker buildx build -f build/Dockerfile -t ghcr.io/skriptvalley/kubescope:latest .

# --- Stage 1: frontend build (runs on the build platform) ---
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Stage 2: Go build, cross-compiled for the target platform ---
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/kubescope ./cmd/kubescope

# --- Stage 3: minimal runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
# Inside the container the network namespace is the isolation boundary; the
# bare-binary default stays 127.0.0.1 (ADR-0005).
ENV KUBESCOPE_LISTEN_ADDR=0.0.0.0:8080
COPY --from=backend /out/kubescope /kubescope
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/kubescope"]
