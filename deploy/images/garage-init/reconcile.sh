#!/bin/sh
set -eu

attempt=1
while ! nc -zvw 2 garage 3903 >/dev/null 2>&1; do
  [ "$attempt" -lt 15 ] || {
    printf '%s\n' 'Garage Admin API did not accept connections within 30 seconds' >&2
    exit 1
  }
  attempt=$((attempt + 1))
  sleep 2
done

garageinit

GARAGE_S3_ENDPOINT=${GARAGE_S3_ENDPOINT:-http://garage:3900} \
GARAGE_S3_REGION=${GARAGE_S3_REGION:-garage} \
GARAGE_BUCKET=${GARAGE_BUCKET:-vermouth-invoices} \
s3probe

printf '%s\n' 'Garage layout, bucket, billing grant, and signed S3 credential proof match'
