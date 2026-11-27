# syntax=docker/dockerfile:1.7

# Stage 1: Build TanStack Start SPA bundle natively on $BUILDPLATFORM
FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS frontend-builder
WORKDIR /src/web

COPY web/package.json web/bun.lock* ./
RUN --mount=type=cache,id=runway-bun,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

COPY web/ ./
RUN bun run generate-routes \
    && bun run build \
    && rm -rf node_modules

# Stage 2: Build static Go 1.27.4 binary with embedded SPA and cross-compile for $TARGETARCH
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS go-builder
WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata \
    && printf 'nonroot:x:65532:65532:nonroot:/:/sbin/nologin\n' > /etc/passwd.nonroot

COPY go.mod go.sum ./
RUN --mount=type=cache,id=runway-gomod,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=frontend-builder /src/web/dist/client/ ./internal/webui/dist/

ARG VERSION=1.0.0
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,id=runway-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=runway-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
        -trimpath \
        -tags netgo,osusergo \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/server ./cmd/server

# Stage 3: Ultra-minimal scratch runtime image (~22 MB, non-root, zero shell)
FROM scratch AS runtime
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-builder /etc/passwd.nonroot /etc/passwd
COPY --from=go-builder /out/server /server

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/server"]
