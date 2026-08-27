#!/bin/sh
set -eu

LOCK_DIRECTORY=/var/run/vermouth-platform-lock
LOCK_LEASE=vermouth-platform-lock
PLATFORM_RUN_ID=
PLATFORM_LOCK_RENEW_PID=
PLATFORM_LOCK_HELD=false
WATCHDOG_PID=

process_start_identity() {
  ps -o lstart= -p "$1" | shasum -a 256 | awk '{print $1}'
}

lock_record() {
  renewed_at=$1
  jq -cn \
    --argjson pid "$$" \
    --arg processStart "$PLATFORM_PROCESS_START" \
    --arg worktree "$ROOT" \
    --arg command "$PLATFORM_COMMAND" \
    --arg runID "$PLATFORM_RUN_ID" \
    --argjson acquiredAt "$PLATFORM_LOCK_ACQUIRED_AT" \
    --argjson renewedAt "$renewed_at" \
    --argjson deadline "$PLATFORM_LOCK_DEADLINE" \
    '{
      pid:$pid,
      process_start:$processStart,
      worktree:$worktree,
      command:$command,
      run_id:$runID,
      acquired_at:$acquiredAt,
      renewed_at:$renewedAt,
      deadline:$deadline
    }'
}

read_host_lock() {
  for name in owner.json owner.json.next; do
    colima ssh -- sudo test -f "$LOCK_DIRECTORY/$name" >/dev/null 2>&1 || continue
    record=$(colima ssh -- sudo cat "$LOCK_DIRECTORY/$name" 2>/dev/null || true)
    if printf '%s' "$record" | jq -e '
      type == "object" and
      (.pid | type == "number") and
      (.process_start | type == "string" and length > 0) and
      (.worktree | type == "string" and length > 0) and
      (.command | type == "string" and length > 0) and
      (.run_id | type == "string" and length > 0) and
      (.acquired_at | type == "number") and
      (.renewed_at | type == "number") and
      (.deadline | type == "number")
    ' >/dev/null 2>&1; then
      printf '%s\n' "$record"
      return
    fi
  done
  return 1
}

write_host_lock() {
  record=$1
  printf '%s\n' "$record" | colima ssh -- sudo tee "$LOCK_DIRECTORY/owner.json.next" >/dev/null
  colima ssh -- sudo chmod 0600 "$LOCK_DIRECTORY/owner.json.next"
  colima ssh -- sudo mv "$LOCK_DIRECTORY/owner.json.next" "$LOCK_DIRECTORY/owner.json"
}

acquire_host_lock() {
  PLATFORM_PROCESS_START=$(process_start_identity "$$")
  PLATFORM_LOCK_ACQUIRED_AT=$(date +%s)
  PLATFORM_LOCK_DEADLINE=$((PLATFORM_LOCK_ACQUIRED_AT + $1))
  if ! colima ssh -- sudo mkdir "$LOCK_DIRECTORY" >/dev/null 2>&1; then
    owner=$(read_host_lock || true)
    [ -n "$owner" ] ||
      fail "platform mutation lock has no complete owner record. Confirm no platform command is running, then restart Colima to clear it."
    command=$(printf '%s' "$owner" | jq -r '.command // "unknown"')
    age=$((PLATFORM_LOCK_ACQUIRED_AT - $(printf '%s' "$owner" | jq -r '.acquired_at // 0')))
    fail "platform mutation is held by $command for $age seconds. Run task platform:lock:clear only after the owner is stale."
  fi
  PLATFORM_LOCK_HELD=true
  write_host_lock "$(lock_record "$PLATFORM_LOCK_ACQUIRED_AT")"
}

ensure_platform_lease() {
  [ "$PLATFORM_LOCK_HELD" = true ] || fail "the host platform lock is not held"
  cluster_running || return 0
  kubectl config get-contexts "$CONTEXT" -o name 2>/dev/null | grep -Fx "$CONTEXT" >/dev/null || return 0
  kube_cluster create namespace "$NAMESPACE" --dry-run=client -o yaml | kube_cluster apply -f - >/dev/null
  holder=$(kube get lease "$LOCK_LEASE" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)
  [ -z "$holder" ] || [ "$holder" = "$PLATFORM_RUN_ID" ] ||
    fail "Kubernetes Lease $LOCK_LEASE is held by run $holder"
  renewed=$(date -u '+%Y-%m-%dT%H:%M:%S.000000Z')
  acquired=$(date -u -r "$PLATFORM_LOCK_ACQUIRED_AT" '+%Y-%m-%dT%H:%M:%S.000000Z')
  lease_duration=$((PLATFORM_LOCK_DEADLINE - PLATFORM_LOCK_ACQUIRED_AT + $(yaml_value "$VERSIONS" deadlines.staleGraceSeconds)))
  kube apply -f - >/dev/null <<EOF
apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  name: $LOCK_LEASE
spec:
  holderIdentity: $PLATFORM_RUN_ID
  leaseDurationSeconds: $lease_duration
  acquireTime: $acquired
  renewTime: $renewed
EOF
}

renew_platform_lock() {
  timer=
  trap 'if [ -n "$timer" ]; then kill "$timer" 2>/dev/null || true; wait "$timer" 2>/dev/null || true; fi; exit 0' TERM INT
  while kill -0 "$$" >/dev/null 2>&1; do
    sleep "$(yaml_value "$VERSIONS" deadlines.lockRenewSeconds)" &
    timer=$!
    wait "$timer" || exit 0
    timer=
    now=$(date +%s)
    write_host_lock "$(lock_record "$now")" || exit 1
    if cluster_running && kubectl config get-contexts "$CONTEXT" -o name 2>/dev/null | grep -Fx "$CONTEXT" >/dev/null; then
      renewed=$(date -u '+%Y-%m-%dT%H:%M:%S.000000Z')
      kube patch lease "$LOCK_LEASE" --type merge \
        -p "{\"spec\":{\"holderIdentity\":\"$PLATFORM_RUN_ID\",\"renewTime\":\"$renewed\"}}" >/dev/null 2>&1 || true
    fi
  done
}

acquire_platform_lock() {
  [ "$PLATFORM_LOCK_HELD" = false ] || return 0
  deadline=$1
  [ -n "$PLATFORM_RUN_ID" ] || PLATFORM_RUN_ID=$(new_run_id)
  acquire_host_lock "$deadline"
  ensure_platform_lease
  renew_platform_lock &
  PLATFORM_LOCK_RENEW_PID=$!
}

release_platform_lock() {
  if [ -n "$PLATFORM_LOCK_RENEW_PID" ]; then
    kill "$PLATFORM_LOCK_RENEW_PID" >/dev/null 2>&1 || true
    wait "$PLATFORM_LOCK_RENEW_PID" 2>/dev/null || true
    PLATFORM_LOCK_RENEW_PID=
  fi
  [ "$PLATFORM_LOCK_HELD" = true ] || return 0
  if cluster_running && kubectl config get-contexts "$CONTEXT" -o name 2>/dev/null | grep -Fx "$CONTEXT" >/dev/null; then
    holder=$(kube get lease "$LOCK_LEASE" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)
    if [ "$holder" = "$PLATFORM_RUN_ID" ]; then
      kube delete lease "$LOCK_LEASE" --ignore-not-found >/dev/null 2>&1 || true
    fi
  fi
  if [ "$PLATFORM_LOCK_HELD" = true ]; then
    owner=$(read_host_lock || true)
    run_id=$(printf '%s' "$owner" | jq -r '.run_id // empty')
    if [ -z "$owner" ] || [ "$run_id" = "$PLATFORM_RUN_ID" ]; then
      colima ssh -- sudo rm -f "$LOCK_DIRECTORY/owner.json" "$LOCK_DIRECTORY/owner.json.next" >/dev/null 2>&1 || true
      colima ssh -- sudo rmdir "$LOCK_DIRECTORY" >/dev/null 2>&1 || true
    fi
  fi
  PLATFORM_LOCK_HELD=false
}

release_platform_lease() {
  if [ -n "$PLATFORM_LOCK_RENEW_PID" ]; then
    kill "$PLATFORM_LOCK_RENEW_PID" >/dev/null 2>&1 || true
    wait "$PLATFORM_LOCK_RENEW_PID" 2>/dev/null || true
  fi
  PLATFORM_LOCK_RENEW_PID=
  holder=$(kube get lease "$LOCK_LEASE" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)
  if [ "$holder" = "$PLATFORM_RUN_ID" ]; then
    kube delete lease "$LOCK_LEASE" --ignore-not-found >/dev/null
  fi
}

cleanup_platform_process() {
  if [ -n "$WATCHDOG_PID" ]; then
    kill "$WATCHDOG_PID" >/dev/null 2>&1 || true
    wait "$WATCHDOG_PID" 2>/dev/null || true
    WATCHDOG_PID=
  fi
  release_platform_lock
}

platform_lock_clear() {
  owner=$(read_host_lock || true)
  [ -n "$owner" ] ||
    fail "no complete host platform lock exists. Confirm no platform command is running, then restart Colima to clear an incomplete record."
  printf '%s\n' "$owner" | jq .
  pid=$(printf '%s' "$owner" | jq -r '.pid')
  recorded_start=$(printf '%s' "$owner" | jq -r '.process_start')
  renewed=$(printf '%s' "$owner" | jq -r '.renewed_at')
  deadline=$(printf '%s' "$owner" | jq -r '.deadline')
  now=$(date +%s)
  grace=$(yaml_value "$VERSIONS" deadlines.staleGraceSeconds)
  if kill -0 "$pid" >/dev/null 2>&1 && [ "$(process_start_identity "$pid")" = "$recorded_start" ]; then
    fail "platform lock owner PID $pid is still live"
  fi
  [ "$now" -gt $((deadline + grace)) ] ||
    fail "platform lock renewal is not beyond its deadline and stale grace"
  confirm_vermouth "Clear stale platform lock"
  if cluster_running && kubectl config get-contexts "$CONTEXT" -o name 2>/dev/null | grep -Fx "$CONTEXT" >/dev/null; then
    kube get lease "$LOCK_LEASE" -o yaml 2>/dev/null || true
    kube delete lease "$LOCK_LEASE" --ignore-not-found >/dev/null
  fi
  colima ssh -- sudo rm -f "$LOCK_DIRECTORY/owner.json" "$LOCK_DIRECTORY/owner.json.next"
  colima ssh -- sudo rmdir "$LOCK_DIRECTORY"
  printf 'Cleared stale platform lock last renewed at %s.\n' "$renewed"
}
