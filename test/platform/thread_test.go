package platform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-13
func TestThreadAcceptsCompactDevelopmentTokenJSON(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":         fixture.path,
		"CALL_LOG":     fixture.callLog,
		"CURL_COUNT":   fixture.count,
		"TOKEN_OUTPUT": `{"schema_version":1,"access_token":"token-1","token_type":"Bearer","expires_in":900,"tutor_id":"tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.NoErrorf(t, result.err, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
	require.Contains(t, result.stdout, "The thread is complete")
	require.NotContains(t, result.stdout, "token-1")
	assertThreadCalls(t, fixture.callLog, "dev:token", "http://vermouth.localhost:8080/api/thread")
}

// covers: AC-11, AC-13
func TestThreadRetriesATemporaryGatewayFailure(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"CURL_COUNT":    fixture.count,
		"CURL_FAILURES": "1",
		"TOKEN_OUTPUT":  `{"schema_version": 1, "access_token": "token-1", "token_type": "Bearer", "expires_in": 900, "tutor_id": "tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.NoErrorf(t, result.err, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "2", strings.TrimSpace(string(count)))
}

// covers: AC-6, AC-13
func TestThreadUsesTheHostTokenAndGatewayWhenRequested(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":              fixture.path,
		"CALL_LOG":          fixture.callLog,
		"CURL_COUNT":        fixture.count,
		"VERMOUTH_DEV_MODE": "host",
		"GATEWAY_HTTP_ADDR": ":9090",
		"TOKEN_OUTPUT":      `{"schema_version": 1, "access_token": "token-host", "token_type": "Bearer", "expires_in": 900, "tutor_id": "tutor-host"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.NoError(t, result.err, result.stderr)
	assertThreadCalls(t, fixture.callLog, "dev:token:host", "http://localhost:9090/api/thread")
}

func TestThreadRejectsTokenOutputWithoutRequiredFields(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":         fixture.path,
		"CALL_LOG":     fixture.callLog,
		"CURL_COUNT":   fixture.count,
		"TOKEN_OUTPUT": `{"schema_version": 1, "tutor_id": "tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.Error(t, result.err)
	require.Contains(t, result.stdout, "devtoken did not answer with a tutor and a token")
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "0", strings.TrimSpace(string(count)))
}

// covers: AC-11, AC-13
func TestThreadStopsAfterTheBoundedGatewayWait(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"CURL_COUNT":    fixture.count,
		"CURL_FAILURES": "99",
		"TOKEN_OUTPUT":  `{"schema_version":1,"access_token":"token-1","tutor_id":"tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.Error(t, result.err)
	require.Contains(t, result.stdout, "The event never reached notifications")
	require.Contains(t, result.stdout, "task platform:logs -- identity")
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "40", strings.TrimSpace(string(count)))
}

type threadFixture struct {
	path    string
	callLog string
	count   string
}

func newThreadFixture(t *testing.T) threadFixture {
	t.Helper()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	count := filepath.Join(directory, "curl-count")
	require.NoError(t, os.WriteFile(count, []byte("0\n"), 0o600))
	writeExecutable(t, directory, "task", `
printf 'task' >>"$CALL_LOG"
for argument in "$@"; do printf ' <%s>' "$argument" >>"$CALL_LOG"; done
printf '\n' >>"$CALL_LOG"
printf '%s\n' "$TOKEN_OUTPUT"
`)
	writeExecutable(t, directory, "curl", `
printf 'curl' >>"$CALL_LOG"
for argument in "$@"; do printf ' <%s>' "$argument" >>"$CALL_LOG"; done
printf '\n' >>"$CALL_LOG"
count=$(cat "$CURL_COUNT")
count=$((count + 1))
printf '%s\n' "$count" >"$CURL_COUNT"
if [ "$count" -le "${CURL_FAILURES:-0}" ]; then exit 7; fi
printf '%s\n' '{"recorded":true}'
`)
	writeExecutable(t, directory, "sleep", ":\n")
	return threadFixture{
		path:    directory + string(os.PathListSeparator) + os.Getenv("PATH"),
		callLog: callLog,
		count:   count,
	}
}

func assertThreadCalls(t *testing.T, path, tokenTask, gateway string) {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	calls := string(content)
	require.Contains(t, calls, "task <--silent> <"+tokenTask+">\n")
	require.Contains(t, calls, "curl <-fsS> <"+gateway+">")
	require.Contains(t, calls, "<-H> <Authorization: Bearer ")
}
