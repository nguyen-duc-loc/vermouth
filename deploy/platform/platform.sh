#!/bin/sh
set -eu

. "$(dirname "$0")/common.sh"
. "$(dirname "$0")/doctor.sh"
. "$(dirname "$0")/images.sh"
. "$(dirname "$0")/secrets.sh"
. "$(dirname "$0")/lifecycle.sh"

command=${1:-}
if [ -n "$command" ]; then shift; fi

case "$command" in
  doctor) doctor ;;
  bootstrap) bootstrap ;;
  dev) platform_dev ;;
  build-native) build_native "${1:-all}" ;;
  build-multi) build_multi ;;
  secrets) prepare_secrets; write_runtime_values ;;
  redeploy) redeploy "${1:-}" ;;
  devtoken) development_token "$@" ;;
  status) platform_status ;;
  logs) platform_logs "$@" ;;
  stop) platform_stop ;;
  clean) platform_clean ;;
  recreate) platform_recreate ;;
  jobs-clean) clean_job "${1:-}" ;;
  validate) validate_platform ;;
  *) fail "unknown command $command" ;;
esac
