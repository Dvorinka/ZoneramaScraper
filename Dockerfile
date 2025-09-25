# Multi-stage build for Go service
FROM golang:1.22-alpine AS build
WORKDIR /app

# Speed up module downloads
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

# Pre-cache modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

# Build the service binary
RUN go build -o app .

# --- Runtime image ---
FROM gcr.io/distroless/static-debian12
WORKDIR /app

# Copy binary from builder
COPY --from=build /app/app /app/app

# Service configuration
ENV PORT=7053
EXPOSE 7053

# Run as non-root where possible
USER 65532:65532

# Start the service
ENTRYPOINT ["/app/app"]
