# Stage 1: Build
FROM golang:1.24-alpine AS builder
ENV GOTOOLCHAIN=auto
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/messaging/ ./services/messaging/
WORKDIR /build/services/messaging
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# Stage 2: Run
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata curl \
    && addgroup -S app && adduser -S app -G app
COPY --from=builder /server /server
USER app
EXPOSE 8082
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8082/health || exit 1
ENTRYPOINT ["/server"]
