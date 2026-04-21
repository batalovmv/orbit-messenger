# Stage 1: Build
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/auth/ ./services/auth/
WORKDIR /build/services/auth
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# Stage 2: Run
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S app && adduser -S app -G app
COPY --from=builder /server /server
COPY migrations/ /migrations/
USER app
EXPOSE 8081
ENTRYPOINT ["/server"]
