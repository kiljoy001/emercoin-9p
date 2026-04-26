# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.26-bookworm AS builder

# Install build tools for CGO
RUN apt-get update && apt-get install -y build-essential

# Create workspace directory
WORKDIR /workspace

# Copy local dependencies
COPY go9p /workspace/go9p
COPY go-dp9ik /workspace/go-dp9ik

# Copy the main application
COPY emercoin-9p /workspace/emercoin-9p

# Build the application
WORKDIR /workspace/emercoin-9p

# Update go.mod to point to the local paths inside the container
RUN go mod edit -replace github.com/knusbaum/go9p=/workspace/go9p && \
    go mod edit -replace github.com/kiljoy001/go-dp9ik=/workspace/go-dp9ik

RUN go mod download
# Enable CGO for go-dp9ik
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/emercoin9p ./cmd/emercoin9p

# Final stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/emercoin9p .

# Port for 9P server
EXPOSE 5640

# Run the server
ENTRYPOINT ["./emercoin9p"]
CMD ["-addr", "0.0.0.0:5640"]
