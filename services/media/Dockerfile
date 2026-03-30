# Stage 1: Build
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/media/ ./services/media/
WORKDIR /build/services/media
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# Stage 2: Run
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata ffmpeg \
    && addgroup -S app && adduser -S app -G app
COPY --from=builder /server /server
USER app
EXPOSE 8083
ENTRYPOINT ["/server"]
