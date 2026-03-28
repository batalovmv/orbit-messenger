# Stage 1: Build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# Stage 2: Run
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S app && adduser -S app -G app
COPY --from=builder /server /server
USER app
EXPOSE ${PORT:-8080}
CMD ["/server"]
