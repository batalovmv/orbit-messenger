# Stage 1: Test (gates the build — tests must pass before binary is produced)
FROM golang:1.24-alpine AS test
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/ai/ ./services/ai/
WORKDIR /build/services/ai
RUN go mod download
RUN go test ./... -count=1

# Stage 2: Build
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY --from=test /build /build
WORKDIR /build/services/ai
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# Stage 2: Run
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata curl \
    && addgroup -S app && adduser -S app -G app
COPY --from=builder /server /server
USER app
EXPOSE 8085
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8085/health || exit 1
ENTRYPOINT ["/server"]
