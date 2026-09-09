package platform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-16
func TestGarageReconcileWaitsThenProvesSignedS3Access(t *testing.T) {
	t.Parallel()

	fixture := newGarageFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"NC_COUNT":      fixture.count,
		"NC_SUCCEED_AT": "3",
	}, []string{repoFile(t, "deploy", "images", "garage-init", "reconcile.sh")})
	require.NoError(t, result.err, result.stderr)
	require.Contains(t, result.stdout, "signed S3 credential proof match")

	calls, err := os.ReadFile(fixture.callLog)
	require.NoError(t, err)
	require.Equal(t,
		"garageinit\n"+
			"s3probe http://garage:3900 garage vermouth-invoices\n",
		string(calls),
	)
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "3", strings.TrimSpace(string(count)))
}

// covers: AC-11, AC-16
func TestGarageReconcileStopsAfterTheBoundedConnectionWindow(t *testing.T) {
	t.Parallel()

	fixture := newGarageFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"NC_COUNT":      fixture.count,
		"NC_SUCCEED_AT": "99",
	}, []string{repoFile(t, "deploy", "images", "garage-init", "reconcile.sh")})

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "did not accept connections within 30 seconds")
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "15", strings.TrimSpace(string(count)))
	_, err = os.Stat(fixture.callLog)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: AC-11, AC-16
func TestGarageReconcileDoesNotProbeAfterInitializerFailure(t *testing.T) {
	t.Parallel()

	fixture := newGarageFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":            fixture.path,
		"CALL_LOG":        fixture.callLog,
		"NC_COUNT":        fixture.count,
		"NC_SUCCEED_AT":   "1",
		"GARAGEINIT_FAIL": "true",
	}, []string{repoFile(t, "deploy", "images", "garage-init", "reconcile.sh")})

	require.Error(t, result.err)
	calls, err := os.ReadFile(fixture.callLog)
	require.NoError(t, err)
	require.Equal(t, "garageinit\n", string(calls))
}

// covers: AC-11, AC-16
func TestGarageReconcileUsesTheConfiguredS3Contract(t *testing.T) {
	t.Parallel()

	fixture := newGarageFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":               fixture.path,
		"CALL_LOG":           fixture.callLog,
		"NC_COUNT":           fixture.count,
		"NC_SUCCEED_AT":      "1",
		"GARAGE_S3_ENDPOINT": "http://garage-alt:4900",
		"GARAGE_S3_REGION":   "local-test",
		"GARAGE_BUCKET":      "invoice-fixtures",
	}, []string{repoFile(t, "deploy", "images", "garage-init", "reconcile.sh")})

	require.NoError(t, result.err, result.stderr)
	calls, err := os.ReadFile(fixture.callLog)
	require.NoError(t, err)
	require.Equal(t,
		"garageinit\n"+
			"s3probe http://garage-alt:4900 local-test invoice-fixtures\n",
		string(calls),
	)
}

// covers: AC-11, AC-16
func TestGarageReconcileDoesNotReportSuccessAfterSignedProbeFailure(t *testing.T) {
	t.Parallel()

	fixture := newGarageFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"NC_COUNT":      fixture.count,
		"NC_SUCCEED_AT": "1",
		"S3PROBE_FAIL":  "true",
	}, []string{repoFile(t, "deploy", "images", "garage-init", "reconcile.sh")})

	require.Error(t, result.err)
	require.NotContains(t, result.stdout, "signed S3 credential proof match")
	calls, err := os.ReadFile(fixture.callLog)
	require.NoError(t, err)
	require.Equal(t,
		"garageinit\n"+
			"s3probe http://garage:3900 garage vermouth-invoices\n",
		string(calls),
	)
}

type garageFixture struct {
	path    string
	callLog string
	count   string
}

func newGarageFixture(t *testing.T) garageFixture {
	t.Helper()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	count := filepath.Join(directory, "count")
	require.NoError(t, os.WriteFile(count, []byte("0\n"), 0o600))
	writeExecutable(t, directory, "nc", `
count=$(cat "$NC_COUNT")
count=$((count + 1))
printf '%s\n' "$count" >"$NC_COUNT"
[ "$count" -ge "$NC_SUCCEED_AT" ]
`)
	writeExecutable(t, directory, "sleep", ":\n")
	writeExecutable(t, directory, "garageinit", `
printf '%s\n' garageinit >>"$CALL_LOG"
[ "${GARAGEINIT_FAIL:-false}" = false ]
`)
	writeExecutable(t, directory, "s3probe", `
printf 's3probe %s %s %s\n' "$GARAGE_S3_ENDPOINT" "$GARAGE_S3_REGION" "$GARAGE_BUCKET" >>"$CALL_LOG"
[ "${S3PROBE_FAIL:-false}" = false ]
`)
	return garageFixture{
		path:    directory + string(os.PathListSeparator) + os.Getenv("PATH"),
		callLog: callLog,
		count:   count,
	}
}
