package platform_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-11, AC-12
func TestLockRecordCarriesTheOwnerIdentity(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "locks.sh"),
		},
		"PLATFORM_PROCESS_START=start-hash\nPLATFORM_COMMAND=dev\nPLATFORM_RUN_ID=run-1\nPLATFORM_LOCK_ACQUIRED_AT=100\nPLATFORM_LOCK_DEADLINE=200\nlock_record 150\n",
		"",
		nil,
	)
	require.NoError(t, result.err, result.stderr)

	var record struct {
		PID          int    `json:"pid"`
		ProcessStart string `json:"process_start"`
		Command      string `json:"command"`
		RunID        string `json:"run_id"`
		AcquiredAt   int    `json:"acquired_at"`
		RenewedAt    int    `json:"renewed_at"`
		Deadline     int    `json:"deadline"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &record))
	require.Positive(t, record.PID)
	require.Equal(t, "start-hash", record.ProcessStart)
	require.Equal(t, "dev", record.Command)
	require.Equal(t, "run-1", record.RunID)
	require.Equal(t, 100, record.AcquiredAt)
	require.Equal(t, 150, record.RenewedAt)
	require.Equal(t, 200, record.Deadline)
}

// covers: AC-11, AC-12
func TestLockClearRefusesALiveOwner(t *testing.T) {
	t.Parallel()

	body := `
PLATFORM_PROCESS_START=$(process_start_identity $$)
owner=$(jq -cn \
  --argjson pid "$$" \
  --arg processStart "$PLATFORM_PROCESS_START" \
  '{pid:$pid,process_start:$processStart,worktree:"repo",command:"dev",run_id:"run-live",acquired_at:1,renewed_at:1,deadline:1}')
read_host_lock() { printf '%s\n' "$owner"; }
platform_lock_clear
`
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "locks.sh"),
		},
		body,
		"",
		nil,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "is still live")
}

// covers: AC-11, AC-12
func TestReleaseDoesNotDeleteAnotherRunLock(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	body := `
PLATFORM_LOCK_HELD=true
PLATFORM_RUN_ID=run-ours
cluster_running() { return 1; }
read_host_lock() { printf '%s\n' '{"run_id":"run-other"}'; }
colima() { printf '%s\n' "$*" >>"$CALL_LOG"; }
release_platform_lock
printf '%s\n' "$PLATFORM_LOCK_HELD"
`
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "locks.sh"),
		},
		body,
		"",
		map[string]string{"CALL_LOG": callLog},
	)
	require.NoError(t, result.err, result.stderr)
	require.Equal(t, "false\n", result.stdout)

	_, err := os.Stat(callLog)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: AC-11, AC-12
func TestAcquireHostLockRejectsAnIncompleteExistingOwner(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "locks.sh"),
		},
		`process_start_identity() { printf '%s\n' start; }
date() { printf '%s\n' 100; }
colima() { return 1; }
read_host_lock() { return 1; }
acquire_host_lock 30
`,
		"",
		nil,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "has no complete owner record")
}

// covers: AC-11, AC-12
func TestLockClearRejectsADeadOwnerInsideTheStaleGrace(t *testing.T) {
	t.Parallel()

	body := `
owner=$(jq -cn '{pid:2147483647,process_start:"dead",worktree:"repo",command:"dev",run_id:"run-dead",acquired_at:50,renewed_at:90,deadline:100}')
read_host_lock() { printf '%s\n' "$owner"; }
date() { printf '%s\n' 105; }
yaml_value() { printf '%s\n' 10; }
platform_lock_clear
`
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "locks.sh"),
		},
		body,
		"",
		nil,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "not beyond its deadline and stale grace")
}
