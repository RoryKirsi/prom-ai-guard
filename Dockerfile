# syntax=docker/dockerfile:1

# ---- build stage ----
# Pinned to the go.mod directive (go 1.24.0). Static, CGO-free binary.
FROM golang:1.24 AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
# Cache modules first for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w" -o /out/prom-ai-guard ./cmd/prom-ai-guard

# ---- runtime stage ----
# distroless static-debian12 ships CA certificates (HTTPS to the LLM works) and a
# non-root user (uid 65532). No shell, no package manager, no baked configs or
# secrets. Compatible with readOnlyRootFilesystem (only mounted volumes are
# written). Alternative: cgr.dev/chainguard/static:latest.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /work
COPY --from=build /out/prom-ai-guard /usr/local/bin/prom-ai-guard
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/prom-ai-guard"]
CMD ["--help"]
