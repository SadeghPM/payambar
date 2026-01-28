# Multi-stage build
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY . .

# Build frontend to cmd/payambar/static
RUN apk add --no-cache make build-base sqlite-dev && make build-frontend

# Build backend with embedded frontend
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o bin/payambar ./cmd/payambar

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root

# Copy binary from builder
COPY --from=builder /app/bin/payambar .

# Create data directory
RUN mkdir -p /data

EXPOSE 8080

ENV PORT=8080 \
    ENVIRONMENT=production \
    DATABASE_PATH=/data/payambar.db \
    FILE_STORAGE_PATH=/data/uploads

VOLUME ["/data"]

CMD ["./payambar"]
