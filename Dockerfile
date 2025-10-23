# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o aero-arc-relay ./cmd/aero-arc-relay

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary
COPY --from=builder /app/aero-arc-relay .

# Copy configuration
COPY --from=builder /app/configs ./configs

# Create log directory
RUN mkdir -p /var/log/aero-arc-relay

# Expose port (if needed for health checks)
EXPOSE 8080

# Run the application
CMD ["./aero-arc-relay", "-config", "configs/config.yaml"]
