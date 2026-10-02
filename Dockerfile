# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
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
