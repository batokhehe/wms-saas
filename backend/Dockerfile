# syntax=docker/dockerfile:1

# ---------- Stage 1: build ----------
# The toolchain image is ~800 MB and is discarded once the binaries are produced.
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Dependencies are copied and downloaded before the source, so an edit to a .go
# file reuses the cached module layer instead of re-downloading everything.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO_ENABLED=0 produces statically linked binaries that run in a scratch-like
# image. -trimpath strips local filesystem paths, and -s -w drop the symbol
# table and DWARF data, cutting binary size roughly in half.
#
# Both binaries are built in one stage so they share the module and build cache.
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/migrate ./cmd/migrate

# ---------- Stage 2: API runtime ----------
# distroless carries no shell and no package manager, which removes most of the
# post-exploitation toolkit an attacker would otherwise find inside the image.
FROM gcr.io/distroless/static-debian12:nonroot AS api

WORKDIR /app

COPY --from=builder /out/api /app/api

# The nonroot tag runs as uid 65532; this image must never run as root.
USER nonroot:nonroot

EXPOSE 8080

# Configuration comes exclusively from the environment in containers. No .env is
# baked into the image, so the same artifact promotes unchanged from dev to prod.
ENTRYPOINT ["/app/api"]
CMD ["-env", ""]

# ---------- Stage 3: migration runtime ----------
# A separate image so the API container ships no binary capable of altering the
# schema, and so migrations can run as an init container or a one-shot job.
FROM gcr.io/distroless/static-debian12:nonroot AS migrate

WORKDIR /app

COPY --from=builder /out/migrate /app/migrate
# The SQL files are data, not code, and must travel with the binary.
COPY --from=builder /src/migrations /app/migrations

USER nonroot:nonroot

ENTRYPOINT ["/app/migrate", "-env", "", "-dir", "/app/migrations"]
CMD ["up"]
