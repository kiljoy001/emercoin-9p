# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.26-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/emercoin9p ./cmd/emercoin9p

# Final stage
FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/emercoin9p .

# Port for 9P server
EXPOSE 5640

# Run the server
# Note: You'll need to provide environment variables at runtime (EMC_RPC_USER, EMC_RPC_PASS, etc.)
ENTRYPOINT ["./emercoin9p"]
CMD ["-addr", "0.0.0.0:5640"]
