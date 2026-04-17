# Root Dockerfile for Saturn/Coolify compatibility.
# Saturn ignores per-service Dockerfile Location for new resources
# and falls back to root Dockerfile. This file builds any service
# via --build-arg SERVICE=<name>.
#
# Usage: docker build --build-arg SERVICE=bots -t orbit-bots .
#
# In Saturn UI: set Build Arg SERVICE=bots (or integrations, etc.)
# After the first successful deploy, Saturn will use the per-service
# Dockerfile from the configured Dockerfile Location.

ARG SERVICE=gateway

FROM golang:1.25-alpine AS builder
ARG SERVICE
WORKDIR /build
COPY pkg/ ./pkg/
COPY services/${SERVICE}/ ./services/${SERVICE}/
WORKDIR /build/services/${SERVICE}
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/main.go

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S app && adduser -S app -G app
COPY --from=builder /server /server
COPY migrations/ /migrations/
USER app
ENTRYPOINT ["/server"]
