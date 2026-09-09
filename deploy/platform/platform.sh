#!/bin/sh
set -eu

. "$(dirname "$0")/common.sh"
. "$(dirname "$0")/locks.sh"
. "$(dirname "$0")/doctor.sh"
. "$(dirname "$0")/images.sh"
. "$(dirname "$0")/secrets.sh"
. "$(dirname "$0")/lifecycle.sh"

command=${1:-}
if [ -n "$command" ]; then shift; fi
PLATFORM_COMMAND=$command
case "$command" in
  doctor|bootstrap|validate|locks-verify) PLATFORM_FAILURE_CODE=2 ;;
  status|logs|stop|clean|recreate|jobs-clean|lock-clear|cache-clean) PLATFORM_FAILURE_CODE=3 ;;
  build-native|build-multi|secrets|dev|redeploy|devtoken) PLATFORM_FAILURE_CODE=4 ;;
  *) PLATFORM_FAILURE_CODE=1 ;;
esac
trap cleanup_platform_process EXIT
trap 'cleanup_platform_process; exit 130' INT
trap 'cleanup_platform_process; exit 143' TERM
trap 'cleanup_platform_process; exit 7' USR1

case "$command" in
  doctor) start_watchdog "$(yaml_value "$VERSIONS" deadlines.doctorSeconds)" ;;
  bootstrap) start_watchdog "$(yaml_value "$VERSIONS" deadlines.bootstrapSeconds)" ;;
  dev)
    if cluster_running && helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth >/dev/null 2>&1; then
      start_watchdog "$(yaml_value "$VERSIONS" deadlines.warmDevSeconds)"
    else
      start_watchdog "$(yaml_value "$VERSIONS" deadlines.coldDevSeconds)"
    fi
    ;;
  build-native|build-multi|secrets|redeploy|devtoken)
    start_watchdog "$(yaml_value "$VERSIONS" deadlines.warmDevSeconds)"
    ;;
  status) start_watchdog "$(yaml_value "$VERSIONS" deadlines.statusSeconds)" ;;
  logs)
    case " $* " in
      *" --follow "*) ;;
      *) start_watchdog "$(yaml_value "$VERSIONS" deadlines.logsSeconds)" ;;
    esac
    ;;
  stop) start_watchdog "$(yaml_value "$VERSIONS" deadlines.stopSeconds)" ;;
  clean) start_watchdog "$(yaml_value "$VERSIONS" deadlines.cleanSeconds)" ;;
  recreate) start_watchdog "$(yaml_value "$VERSIONS" deadlines.recreateSeconds)" ;;
  jobs-clean) start_watchdog "$(yaml_value "$VERSIONS" deadlines.jobsCleanSeconds)" ;;
  cache-clean) start_watchdog "$(yaml_value "$VERSIONS" deadlines.cacheCleanSeconds)" ;;
  measure) start_watchdog "$(yaml_value "$VERSIONS" deadlines.measureSeconds)" ;;
esac

case "$command" in
  bootstrap|dev|clean|recreate|cache-clean)
    if ! colima status >/dev/null 2>&1; then colima start; fi
    ;;
esac

case "$command" in
  bootstrap|dev|recreate) VERIFY_PORT_80_LISTENERS=true ;;
esac

case "$command" in
  bootstrap|dev|build-native|build-multi|secrets|redeploy|devtoken|stop|clean|recreate|jobs-clean|lock-clear|cache-clean)
    capture_runtime_configuration
    ;;
esac

case "$command" in
  bootstrap) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.bootstrapSeconds)" ;;
  dev|build-multi) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.coldDevSeconds)" ;;
  build-native|secrets|redeploy|devtoken) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.warmDevSeconds)" ;;
  stop) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.stopSeconds)" ;;
  clean) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.cleanSeconds)" ;;
  recreate) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.recreateSeconds)" ;;
  jobs-clean) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.jobsCleanSeconds)" ;;
  cache-clean) acquire_platform_lock "$(yaml_value "$VERSIONS" deadlines.cacheCleanSeconds)" ;;
esac

case "$command" in
  doctor) doctor ;;
  bootstrap) bootstrap ;;
  dev) platform_dev ;;
  build-native) build_native "${1:-all}" ;;
  build-multi) build_multi ;;
  locks-verify) verify_external_locks ;;
  secrets) prepare_secrets; write_runtime_values "$PLATFORM_RUN_ID" ;;
  redeploy) redeploy "${1:-}" ;;
  devtoken) development_token "$@" ;;
  status) platform_status ;;
  logs) platform_logs "$@" ;;
  stop) platform_stop ;;
  clean) platform_clean ;;
  recreate) platform_recreate ;;
  jobs-clean) clean_job "${1:-}" ;;
  lock-clear) platform_lock_clear ;;
  cache-clean) clean_build_cache ;;
  measure) measure_platform ;;
  validate) validate_platform ;;
  *) fail "unknown command $command" ;;
esac

verify_runtime_configuration_unchanged
