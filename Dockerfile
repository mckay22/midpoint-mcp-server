# Build a fully static binary, then ship it on scratch.
FROM golang:1.25 AS build

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO off → static, pure-Go binary that runs on scratch.
ARG VERSION=docker
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" -o /out/midpoint-mcp-server .

# Runtime: scratch (nothing but the binary and CA certs).
FROM scratch

# CA certificates so the server can reach midPoint over HTTPS.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/midpoint-mcp-server /midpoint-mcp-server

# Run unprivileged (scratch has no users; use the conventional "nobody" uid).
USER 65534:65534

# Default transport is stdio (personal mode). Configuration is via MIDPOINT_*
# environment variables at runtime. HTTP mode (--http) is loopback-only until
# per-request auth lands (PLAN.md M4.5); to use it, run with host networking and
# bind 127.0.0.1.
ENTRYPOINT ["/midpoint-mcp-server"]
