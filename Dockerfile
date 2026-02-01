# ----------------------------
# Build stage
# ----------------------------
FROM golang:1.24-alpine AS builder

# Install git (needed for go modules)
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o server ./cmd/server/main.go


# ----------------------------
# Runtime stage
# ----------------------------
FROM golang:1.24-alpine

# Install CA certificates (important for HTTPS, APIs, Kite, etc.)
# RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server .

# Copy env file only if you want it inside image
# (usually env vars are injected via docker-compose)
COPY .env .env

# Expose your app port (change if different)
EXPOSE 8080

# Run the server
CMD ["./server"]
