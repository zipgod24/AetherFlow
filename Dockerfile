# syntax=docker/dockerfile:1.7
#
# Multi-binary image for AetherFlow. Build targets one binary per stage so a
# single `docker build --target retriever-agent` produces a minimal image.
#
# All Go binaries are statically linked, runs under non-root, distroless base.

FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .

ARG BIN
RUN test -n "$BIN" || (echo "build arg BIN is required (e.g. retriever-agent)"; exit 1)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${BIN}

# -------- runtime --------
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/app /app
# Some binaries (api-gateway) want to serve static assets in /app/web/ui
COPY --from=builder /src/web/ui /app/web/ui
USER nonroot:nonroot
ENTRYPOINT ["/app"]
