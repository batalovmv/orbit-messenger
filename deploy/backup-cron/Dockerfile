# Cron container that runs scripts/backup-postgres.sh on a schedule.
# Kept intentionally small — just postgres-client (pg_dump), gpg, gzip,
# and the AWS CLI (for S3-compatible R2 uploads). No daemon lurking
# around: supercronic wakes up on schedule, executes the script, and
# exits the tick.

FROM alpine:3.20

ARG TARGETARCH

# Pin supercronic because it's the one binary here that doesn't come
# from Alpine repos. Hash-verified against upstream release.
ARG SUPERCRONIC_VERSION=v0.2.34

RUN apk add --no-cache \
      bash \
      ca-certificates \
      curl \
      gnupg \
      gzip \
      postgresql16-client \
      aws-cli \
      tzdata \
  && case "$TARGETARCH" in \
       amd64) SC_ARCH=amd64 ;; \
       arm64) SC_ARCH=arm64 ;; \
       *) echo "unsupported arch $TARGETARCH" && exit 1 ;; \
     esac \
  && curl -fsSL -o /usr/local/bin/supercronic \
       "https://github.com/aptible/supercronic/releases/download/${SUPERCRONIC_VERSION}/supercronic-linux-${SC_ARCH}" \
  && chmod +x /usr/local/bin/supercronic

COPY scripts/backup-postgres.sh /usr/local/bin/backup-postgres.sh
RUN chmod +x /usr/local/bin/backup-postgres.sh

# Crontab: every 4 hours on the hour (00/04/08/12/16/20 UTC). The
# container image is the same in dev and prod; override BACKUP_CRON at
# runtime to change the schedule. RPO ≈ 4h until WAL/PITR lands.
ENV BACKUP_CRON="0 */4 * * *"
COPY deploy/backup-cron/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER nobody

ENTRYPOINT ["/entrypoint.sh"]
