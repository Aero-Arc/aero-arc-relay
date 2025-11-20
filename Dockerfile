############################
# Build stage
############################
FROM golang:1.24-bullseye AS builder

WORKDIR /src

# Copy go module manifests first so we can cache deps
COPY go.mod go.sum ./
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config librdkafka-dev build-essential ca-certificates && rm -rf /var/lib/apt/lists/*
RUN go mod download

# Copy the rest of the source
COPY . .

# Build the relay binary
RUN GOOS=linux GOARCH=amd64 go build -o /out/aero-arc-relay ./cmd/aero-arc-relay

############################
# Runtime stage
############################
FROM gcr.io/distroless/cc-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/aero-arc-relay /usr/local/bin/aero-arc-relay
COPY --from=builder /src/configs/config.yaml /etc/aero-arc-relay/config.yaml
# Optional: copy default configs (override via mounted volume or env)
# COPY configs /etc/aero-arc-relay/

EXPOSE 2112

ENTRYPOINT ["/usr/local/bin/aero-arc-relay"]
CMD ["-config", "/etc/aero-arc-relay/config.yaml"]
