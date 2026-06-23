FROM golang:1.25-bookworm AS builder

# Optimise the generated binary for the AMD64 v3 micro-architecture.
ENV GOAMD64=v3

WORKDIR /build

# Download modules in a separate layer so they're cached across source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /out && go build -v -o /out/bitrise-mcp-dev-environments .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/bitrise-mcp-dev-environments /bitrise-mcp-dev-environments

# This image runs the HTTP transport (the hosted deployment). Setting ADDR is
# what selects HTTP over stdio; local users run the stdio transport via
# `go run` instead, where ADDR is unset. All OAuth / workspace settings come
# from the deployment environment (SERVER_BASE_URL, EXTERNAL_OAUTH_ISSUER,
# OIDC_TOKEN_ENDPOINT, ...), never baked into the image.
ENV ADDR="0.0.0.0:8000"
EXPOSE 8000

CMD ["/bitrise-mcp-dev-environments"]
