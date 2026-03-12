# syntax=docker/dockerfile:1

# IMG-001: Multi-stage build (builder + runtime)
# IMG-002: Go Alpine pinned to project Go version, native build platform
FROM --platform=$BUILDPLATFORM golang:1.26.1-alpine AS builder

# IMG-008: Cross-compilation args (auto-populated by BuildKit)
ARG TARGETOS
ARG TARGETARCH
# VER-007: Build-time version injection
ARG VERSION=development

WORKDIR /src

# IMG-009: Dependency layer cached separately
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# IMG-010: Build cache mount for incremental rebuilds
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X github.com/infobloxopen/ibcq-source-oci/plugin.Version=${VERSION}" \
    -o /cq-source-oci .

# IMG-003: Distroless static runtime (non-root, no shell)
FROM gcr.io/distroless/static-debian12:nonroot

# IMG-011: Entrypoint = plugin binary
COPY --from=builder /cq-source-oci /cq-source-oci

# IMG-013: Document default gRPC port
EXPOSE 7777

# IMG-012/IMG-014/IMG-015: Default serve on [::]:7777, runs as nonroot (UID 65534)
ENTRYPOINT ["/cq-source-oci"]
CMD ["serve", "--address", "[::]:7777"]
