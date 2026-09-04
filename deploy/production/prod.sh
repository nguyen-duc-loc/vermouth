#!/bin/sh
set -eu

PROD_SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
export PROD_SCRIPT_DIR
. "$(dirname "$0")/prod-common.sh"
. "$(dirname "$0")/prod-doctor.sh"
. "$(dirname "$0")/prod-bootstrap.sh"
. "$(dirname "$0")/prod-operations.sh"

command=${1:-}
if [ -n "$command" ]; then shift; fi

case "$command" in
  doctor) prod_doctor ;;
  bootstrap) prod_bootstrap ;;
  deploy) prod_deploy "$@" ;;
  status) prod_status ;;
  logs) prod_logs "$@" ;;
  rollback) prod_rollback ;;
  export) prod_export "$@" ;;
  restore) prod_restore "$@" ;;
  *) prod_fail "unknown command $command" ;;
esac
