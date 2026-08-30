# syntax=docker/dockerfile:1

# Build stage: keeps the Go toolchain out of the final image.
FROM golang:1.25.13-alpine3.23 AS builder

WORKDIR /src

# Copy dependency manifests first to maximize Docker layer cache reuse.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN go mod tidy \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/auth-service .

# Runtime stage: small image with only certificates, wget for healthchecks,
# a non-root user, and the compiled binary.
FROM alpine:3.23

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S app \
    && adduser -S -G app app

WORKDIR /app
COPY --from=builder /out/auth-service /app/auth-service

USER app

ENV PORT=8081
EXPOSE 8081

CMD ["/app/auth-service"]
