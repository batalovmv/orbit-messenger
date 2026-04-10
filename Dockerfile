# Root Dockerfile for Saturn/Coolify compatibility.
# Saturn sometimes ignores per-service Dockerfile Location setting and looks
# for a root Dockerfile. This file delegates to the actual service Dockerfiles.
#
# For local development use docker-compose.yml which references per-service Dockerfiles.

ARG SERVICE=stub

# --- Stub (default) ---
FROM golang:1.25-alpine AS stub-builder
WORKDIR /build
COPY go.mod ./
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Auth ---
FROM golang:1.25-alpine AS auth-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/auth/ ./services/auth/
WORKDIR /build/services/auth
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Messaging ---
FROM golang:1.25-alpine AS messaging-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/messaging/ ./services/messaging/
WORKDIR /build/services/messaging
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Media ---
FROM golang:1.25-alpine AS media-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/media/ ./services/media/
WORKDIR /build/services/media
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Gateway ---
FROM golang:1.25-alpine AS gateway-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/gateway/ ./services/gateway/
WORKDIR /build/services/gateway
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Calls ---
FROM golang:1.25-alpine AS calls-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/calls/ ./services/calls/
WORKDIR /build/services/calls
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Bots ---
FROM golang:1.25-alpine AS bots-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/bots/ ./services/bots/
WORKDIR /build/services/bots
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Integrations ---
FROM golang:1.25-alpine AS integrations-builder
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/integrations/ ./services/integrations/
WORKDIR /build/services/integrations
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

# --- Final stage ---
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S app && adduser -S app -G app
COPY --from=${SERVICE}-builder /server /server
USER app
EXPOSE ${PORT:-8080}
ENTRYPOINT ["/server"]
