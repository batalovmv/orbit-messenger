# Custom NATS image for Saturn deploys.
#
# Saturn's UI does not expose a CLI-args / start-command override for
# off-the-shelf images. Pulling raw `nats:latest` therefore boots the
# server with default args — no `-js`, no auth — and the gateway falls
# back to core NATS losing all replay guarantees. The 2026-05-04 prod
# log audit caught this; this image is the fix.
#
# Local docker-compose is kept on the same image (build:) so dev and
# Saturn stay byte-for-byte identical.

FROM nats:2-alpine

COPY nats.conf /etc/nats/nats.conf

# JetStream needs a writable store dir; declare it as a volume so
# Saturn can wire managed storage and local compose can mount a named
# volume.
VOLUME ["/data"]

# Client + monitoring ports. 6222 is the cluster route port — leave
# unexposed for now since we run a single node, open it explicitly
# when we add HA.
EXPOSE 4222 8222

# Fail loud and early if NATS_TOKEN is missing rather than silently
# starting an open server. The exec form keeps PID 1 on nats-server
# so docker stop sends SIGTERM directly (graceful JetStream flush).
ENTRYPOINT ["/bin/sh", "-c", "if [ -z \"$NATS_TOKEN\" ]; then echo 'FATAL: NATS_TOKEN must be set' >&2; exit 1; fi; exec nats-server -c /etc/nats/nats.conf"]
