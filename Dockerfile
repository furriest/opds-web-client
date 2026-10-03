# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# git is required by go mod download; ca-certificates for HTTPS fetches
RUN apk add --no-cache git ca-certificates curl

COPY go.mod ./
# GOPROXY=direct: load modules from GitHub/etc directly, bypassing proxy.golang.org
# (storage.googleapis.com used by the default proxy is often blocked by VPNs)
# GONOSUMDB=*: skip checksum DB verification (still verifies module hashes locally)
ENV GOPROXY=direct GONOSUMDB=*
RUN go mod download

COPY . .

# Download epub.js and its JSZip dependency at build time (no CDN at runtime)
RUN mkdir -p static/lib && \
    curl -fsSL -o static/lib/jszip.min.js \
    https://cdn.jsdelivr.net/npm/jszip@3.10.1/dist/jszip.min.js && \
    curl -fsSL -o static/lib/epub.min.js \
    https://cdn.jsdelivr.net/npm/epubjs@0.3.93/dist/epub.min.js

# -mod=mod allows go to create/update go.sum during build if needed
RUN CGO_ENABLED=0 GOOS=linux go build \
    -mod=mod \
    -ldflags="-s -w" \
    -trimpath \
    -o opds-server .

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM scratch

# TLS root certificates for HTTPS requests to OPDS servers
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/opds-server /opds-server

EXPOSE 80

ENTRYPOINT ["/opds-server"]
