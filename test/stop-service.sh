#!/usr/bin/env sh
# Stop one service and wait until it is really gone.
#
# Waiting matters rather than being tidy: a replay must not start while the
# consumer still holds its group (STK-22), and the same graceful stop is what
# Kubernetes will send in feature 5.
set -e

RUNDIR=$1
NAME=$2
PIDFILE="$RUNDIR/$NAME.pid"

PID=""
if [ -f "$PIDFILE" ]; then
  PID=$(cat "$PIDFILE")
fi

# A stale or missing pid file must not leave a service running, so fall back to
# matching the binary itself.
if [ -z "$PID" ] || ! kill -0 "$PID" 2>/dev/null; then
  PID=$(pgrep -x -f "./bin/$NAME" || true)
fi
if [ -z "$PID" ]; then
  rm -f "$PIDFILE"
  echo "$NAME was not running"
  exit 0
fi

kill "$PID" 2>/dev/null || true
i=0
while kill -0 "$PID" 2>/dev/null; do
  if [ "$i" -ge 20 ]; then
    echo "$NAME did not stop in 20s, forcing it"
    kill -9 "$PID" 2>/dev/null || true
    break
  fi
  sleep 1
  i=$((i + 1))
done
rm -f "$PIDFILE"
echo "stopped $NAME"
