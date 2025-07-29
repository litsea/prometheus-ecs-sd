# Build stage
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /build

ARG GOPROXY
ENV GOPROXY=${GOPROXY:-} GOCACHE=/root/.cache/go-build

# Install build dependencies
RUN apk add git make

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN make build

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates curl bash

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/prometheus-ecs-sd ./

# Set environment variables
ENV TZ=UTC

# Run the application
ENTRYPOINT ["/app/prometheus-ecs-sd"]
