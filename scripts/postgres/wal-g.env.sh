#!/bin/bash
export AWS_ENDPOINT=${R2_ENDPOINT}
export AWS_ACCESS_KEY_ID=${R2_ACCESS_KEY_ID}
export AWS_SECRET_ACCESS_KEY=${R2_SECRET_ACCESS_KEY}
export AWS_S3_FORCE_PATH_STYLE=true
export AWS_REGION=auto
export WALG_S3_PREFIX=s3://${R2_BACKUP_WAL_BUCKET:-orbit-backups}/wal-g
export PGHOST=/var/run/postgresql
# wal-g defaults PGUSER/PGDATABASE to "postgres" — our cluster uses
# the application credentials. Source these from the standard
# POSTGRES_* env the official image already passes through, falling
# back to "orbit" so the cron works on a stock dev volume.
export PGUSER=${POSTGRES_USER:-orbit}
export PGDATABASE=${POSTGRES_DB:-orbit}
