#!/bin/sh

prod_deploy() {
  prod_operator_init
  prod_need gh
  ref=${1:-}
  [ -n "$ref" ] || prod_fail "name a Git ref, for example: task prod:deploy -- main"
  prod_single_line_matches "$ref" '^[A-Za-z0-9._/-]+$' || prod_fail "the Git ref is invalid"
  printf '%s' 'Type production to publish and deploy: '
  IFS= read -r confirmation
  [ "$confirmation" = production ] || prod_fail "deployment cancelled"
  gh workflow run production.yml --ref "$ref" \
    --raw-field source_ref="$ref" --raw-field confirmation=production
  printf '%s\n' "Production workflow requested for $ref. You may run gh run watch --exit-status."
}

prod_status() {
  prod_operator_init
  prod_ssh 'sudo --non-interactive /usr/local/sbin/vermouth-prod-ops status'
}

prod_logs() {
  prod_operator_init
  target=${1:-}
  [ -n "$target" ] || prod_fail "name a production log target"
  shift
  case "$target" in
    web|gateway|identity|teaching|billing|notifications|postgres-identity|postgres-teaching|postgres-billing|postgres-notifications|redpanda|garage|traefik|garage-init|all) ;;
    migrate-*)
      prod_single_line_matches "$target" '^migrate-(identity|teaching|billing|notifications)-[1-9][0-9]*-[1-9][0-9]*$' ||
        prod_fail "unknown production log target $target"
      ;;
    *) prod_fail "unknown production log target $target" ;;
  esac
  remote="sudo --non-interactive /usr/local/sbin/vermouth-prod-ops logs $target"
  for option in "$@"; do
    case "$option" in
      --previous|--follow) ;;
      --since=*)
        prod_single_line_matches "$option" '^--since=[1-9][0-9]*[smh]$' ||
          prod_fail "unknown production log option $option"
        ;;
      *) prod_fail "unknown production log option $option" ;;
    esac
    remote="$remote $option"
  done
  prod_ssh "$remote"
}

prod_rollback() {
  prod_operator_init
  prod_ssh 'sudo --non-interactive /usr/local/sbin/vermouth-prod-ops rollback'
}

prod_validate_age_identity() {
  prod_require_env PROD_AGE_IDENTITY
  case "$PROD_AGE_IDENTITY" in
    /*) ;;
    *) prod_fail "PROD_AGE_IDENTITY must be an absolute path" ;;
  esac
  [ -f "$PROD_AGE_IDENTITY" ] && [ ! -L "$PROD_AGE_IDENTITY" ] ||
    prod_fail "PROD_AGE_IDENTITY must be one regular file without a symbolic link"
  case "$(uname -s)" in
    Darwin)
      identity_owner=$(stat -f '%u' "$PROD_AGE_IDENTITY")
      identity_mode=$(stat -f '%Lp' "$PROD_AGE_IDENTITY")
      ;;
    *)
      identity_owner=$(stat -c '%u' "$PROD_AGE_IDENTITY")
      identity_mode=$(stat -c '%a' "$PROD_AGE_IDENTITY")
      ;;
  esac
  [ "$identity_owner" = "$(id -u)" ] || prod_fail "PROD_AGE_IDENTITY must be owned by the current user"
  [ "$identity_mode" = 600 ] || prod_fail "PROD_AGE_IDENTITY must have mode 0600"
  derived_recipient=$(age-keygen -y "$PROD_AGE_IDENTITY") || prod_fail "the age identity cannot derive a recipient"
  identity_lines=$(grep -Ec '^AGE-SECRET-KEY-1[0-9A-Z]+$' "$PROD_AGE_IDENTITY" || true)
  [ "$identity_lines" -eq 1 ] || prod_fail "PROD_AGE_IDENTITY must contain exactly one native age identity"
  [ "$(printf '%s\n' "$derived_recipient" | awk 'NF {count++} END {print count+0}')" -eq 1 ] ||
    prod_fail "PROD_AGE_IDENTITY must contain exactly one native age identity"
  [ "$derived_recipient" = "$PROD_AGE_RECIPIENT" ] ||
    prod_fail "PROD_AGE_IDENTITY does not match PROD_AGE_RECIPIENT"
}

prod_archive_destination() {
  requested=$1
  parent=$(dirname "$requested")
  name=$(basename "$requested")
  [ -n "$name" ] && [ "$name" != . ] && [ "$name" != .. ] || prod_fail "the archive destination is invalid"
  parent=$(CDPATH= cd -- "$parent" && pwd -P) || prod_fail "the archive destination directory does not exist"
  printf '%s/%s\n' "$parent" "$name"
}

prod_export() {
  prod_init
  for tool in age age-keygen awk basename dirname grep id mktemp stat uname zstd; do
    prod_need "$tool"
  done
  [ "$#" -eq 1 ] || prod_fail "name one new export path, for example: task prod:export -- /secure/vermouth.tar.zst.age"
  prod_validate_age_identity
  destination=$(prod_archive_destination "$1")
  [ ! -e "$destination" ] && [ ! -L "$destination" ] || prod_fail "the export destination already exists"
  temporary=$(mktemp "$destination.tmp.XXXXXX")
  chmod 0600 "$temporary"
  cleanup_export() {
    rm -f "$temporary"
  }
  trap cleanup_export EXIT HUP INT TERM
  remote_environment=$(prod_remote_environment)
  prod_ssh "sudo --non-interactive env $remote_environment /usr/local/sbin/vermouth-prod-ops export" |
    age --encrypt --recipient "$PROD_AGE_RECIPIENT" >"$temporary"
  age --decrypt --identity "$PROD_AGE_IDENTITY" "$temporary" |
    prod_platform_config production-archive-validate >/dev/null
  mv "$temporary" "$destination"
  trap - EXIT HUP INT TERM
  printf '%s\n' "Encrypted production export verified at $destination"
  if ! safety_state=$(prod_ssh 'sudo --non-interactive /usr/local/sbin/vermouth-prod-ops restore-safety-status'); then
    printf '%s\n' 'The export is safe, but the prior restore safety state could not be inspected.' >&2
    return
  fi
  if [ -n "$safety_state" ]; then
    printf '%s\n' 'A prior restore safety state is still retained:'
    printf '%s\n' "$safety_state"
    printf '%s' 'Type vermouth to remove it after this verified export, or press return to keep it: '
    IFS= read -r cleanup_confirmation || cleanup_confirmation=
    if [ "$cleanup_confirmation" = vermouth ]; then
      prod_ssh 'sudo --non-interactive /usr/local/sbin/vermouth-prod-ops restore-safety-clean vermouth'
    else
      printf '%s\n' 'The prior restore safety state was kept.'
    fi
  fi
}

prod_restore() {
  prod_init
  for tool in age age-keygen awk grep id sed stat uname zstd; do
    prod_need "$tool"
  done
  [ "$#" -eq 1 ] || prod_fail "name one encrypted export, for example: task prod:restore -- /secure/vermouth.tar.zst.age"
  archive=$1
  [ -f "$archive" ] && [ ! -L "$archive" ] || prod_fail "$archive is missing or unsafe"
  prod_validate_age_identity
  archive_sha=$(age --decrypt --identity "$PROD_AGE_IDENTITY" "$archive" |
    prod_platform_config production-archive-validate)
  printf '%s\n' "Authenticated export SHA256: $archive_sha"
  remote_environment=$(prod_remote_environment)
  receive_output=$(age --decrypt --identity "$PROD_AGE_IDENTITY" "$archive" |
    prod_ssh "sudo --non-interactive env $remote_environment /usr/local/sbin/vermouth-prod-ops restore-receive $archive_sha")
  printf '%s\n' "$receive_output"
  restore_run_id=$(printf '%s\n' "$receive_output" | sed -n 's/^RESTORE_RUN_ID=//p')
  printf '%s' "$restore_run_id" | grep -Eq '^[A-Za-z0-9.]+$' || prod_fail "the VM returned no valid restore run ID"
  printf '%s' 'Type vermouth to replace production state with this export: '
  IFS= read -r confirmation
  if [ "$confirmation" != vermouth ]; then
    prod_ssh "sudo --non-interactive env $remote_environment /usr/local/sbin/vermouth-prod-ops restore-discard $restore_run_id" >/dev/null || true
    prod_fail "restore cancelled and its staged plaintext archive was discarded"
  fi
  prod_ssh "sudo --non-interactive env $remote_environment /usr/local/sbin/vermouth-prod-ops restore-apply $restore_run_id vermouth"
}
