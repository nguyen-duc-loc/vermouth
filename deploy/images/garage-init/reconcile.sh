#!/bin/sh
set -eu

bucket=${GARAGE_BUCKET:-vermouth-invoices}
key_name=${GARAGE_KEY_NAME:-billing}
zone=${GARAGE_ZONE:-local}
capacity=${GARAGE_CAPACITY:-5G}

status=$(garage status)
node_id=$(printf '%s\n' "$status" | awk '/^[0-9a-f][0-9a-f]*/ {print $1; exit}')
if [ -z "$node_id" ]; then
  echo "Garage did not report a node id"
  exit 1
fi

layout=$(garage layout show)
if ! printf '%s\n' "$layout" | grep -F "$node_id" | grep -F "$zone" | grep -F "$capacity" >/dev/null; then
  garage layout assign "$node_id" -z "$zone" -c "$capacity"
  garage layout apply
fi

if ! garage bucket info "$bucket" >/tmp/bucket 2>/dev/null; then
  garage bucket create "$bucket"
  garage bucket info "$bucket" >/tmp/bucket
fi

if garage key info "$key_name" >/tmp/key 2>/dev/null; then
  if ! grep -F "$GARAGE_ACCESS_KEY_ID" /tmp/key >/dev/null; then
    echo "Garage key $key_name exists with a conflicting access key"
    exit 1
  fi
else
  garage key import --yes "$GARAGE_ACCESS_KEY_ID" "$GARAGE_SECRET_ACCESS_KEY" -n "$key_name"
fi

garage bucket info "$bucket" >/tmp/bucket
if ! grep -F "$GARAGE_ACCESS_KEY_ID" /tmp/bucket | grep -E 'R.*W|read.*write' >/dev/null; then
  garage bucket allow --key "$key_name" --read --write "$bucket"
fi

garage bucket info "$bucket"
