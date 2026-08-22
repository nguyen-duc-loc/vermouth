#!/usr/bin/env sh
# Start one service in the background, record its pid, and confirm it is up.
#
# It confirms rather than assumes, because a service that dies on a missing
# environment variable should say so here (STK-8) instead of failing later as a
# confusing connection refused.
set -e

RUNDIR=$1
LOGDIR=$2
NAME=$3

mkdir -p "$RUNDIR" "$LOGDIR"
PIDFILE="$RUNDIR/$NAME.pid"

if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "$NAME already running as $(cat "$PIDFILE")"
  exit 0
fi

# Refuse a second copy even when the pid file was lost. Two copies of a
# consuming service put two members in one group, which parks the group in a
# rebalance and makes a stop take a minute; for notifications it would also
# break STK-23, which keeps it at exactly one replica.
RUNNING=$(pgrep -x -f "./bin/$NAME" || true)
if [ -n "$RUNNING" ]; then
  echo "$RUNNING" > "$PIDFILE"
  echo "$NAME already running as $RUNNING, adopted it"
  exit 0
fi

nohup "./bin/$NAME" >> "$LOGDIR/$NAME.log" 2>&1 &
PID=$!
echo "$PID" > "$PIDFILE"

sleep 1
if ! kill -0 "$PID" 2>/dev/null; then
  rm -f "$PIDFILE"
  echo "$NAME did not stay up. The last lines of $LOGDIR/$NAME.log:"
  tail -5 "$LOGDIR/$NAME.log"
  exit 1
fi
echo "started $NAME as $PID"
