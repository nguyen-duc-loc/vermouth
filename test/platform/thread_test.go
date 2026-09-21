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
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"CURL_COUNT":    fixture.count,
		"PROFILE_STATE": fixture.profile,
		"TOKEN_OUTPUT":  `{"schema_version":1,"access_token":"token-1","token_type":"Bearer","expires_in":900,"tutor_id":"tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.NoErrorf(t, result.err, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
	require.Contains(t, result.stdout, "The teaching thread is complete")
	require.NotContains(t, result.stdout, "token-1")
	assertThreadCalls(t, fixture.callLog, "dev:token", "http://vermouth.localhost:8080")
}

// covers: AC-11, AC-13
func TestThreadRetriesATemporaryGatewayFailure(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"CURL_COUNT":    fixture.count,
		"PROFILE_STATE": fixture.profile,
		"CURL_FAILURES": "1",
		"TOKEN_OUTPUT":  `{"schema_version": 1, "access_token": "token-1", "token_type": "Bearer", "expires_in": 900, "tutor_id": "tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.NoErrorf(t, result.err, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "19", strings.TrimSpace(string(count)))
}

// covers: AC-6, AC-13
func TestThreadUsesTheHostTokenAndGatewayWhenRequested(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":              fixture.path,
		"CALL_LOG":          fixture.callLog,
		"CURL_COUNT":        fixture.count,
		"PROFILE_STATE":     fixture.profile,
		"VERMOUTH_DEV_MODE": "host",
		"GATEWAY_HTTP_ADDR": ":9090",
		"TOKEN_OUTPUT":      `{"schema_version": 1, "access_token": "token-host", "token_type": "Bearer", "expires_in": 900, "tutor_id": "tutor-host"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.NoError(t, result.err, result.stderr)
	assertThreadCalls(t, fixture.callLog, "dev:token:host", "http://localhost:9090")
}

func TestThreadRejectsTokenOutputWithoutRequiredFields(t *testing.T) {
	t.Parallel()

	fixture := newThreadFixture(t)
	result := runCommand(t, "", map[string]string{
		"PATH":          fixture.path,
		"CALL_LOG":      fixture.callLog,
		"CURL_COUNT":    fixture.count,
		"PROFILE_STATE": fixture.profile,
		"TOKEN_OUTPUT":  `{"schema_version": 1, "tutor_id": "tutor-1"}`,
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
		"PROFILE_STATE": fixture.profile,
		"CURL_FAILURES": "99",
		"TOKEN_OUTPUT":  `{"schema_version":1,"access_token":"token-1","tutor_id":"tutor-1"}`,
	}, []string{repoFile(t, "test", "thread.sh")})

	require.Error(t, result.err)
	require.Contains(t, result.stdout, "The home read never became available")
	count, err := os.ReadFile(fixture.count)
	require.NoError(t, err)
	require.Equal(t, "40", strings.TrimSpace(string(count)))
}

type threadFixture struct {
	path    string
	callLog string
	count   string
	profile string
}

func newThreadFixture(t *testing.T) threadFixture {
	t.Helper()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	count := filepath.Join(directory, "curl-count")
	profile := filepath.Join(directory, "profile-state")
	require.NoError(t, os.WriteFile(count, []byte("0\n"), 0o600))
	require.NoError(t, os.WriteFile(profile, []byte("0\n"), 0o600))
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
calls=" $* "
case "$calls" in
  *" -X PUT "*"/api/invoice-profile"*)
    printf '1\n' >"$PROFILE_STATE"
    printf '%s\n' '{"legal_name":"Thread Tutor","contact_line":"thread@example.com","bank_code":"970436","bank_name":"Vietcombank","bank_account_number":"THREAD123","bank_account_holder":"THREAD TUTOR","revision":1,"is_complete":true,"missing_fields":[],"bank_status":"active"}'
    ;;
  *"/api/invoice-profile"*)
    if [ "$(cat "$PROFILE_STATE")" = 1 ]; then
      printf '%s\n' '{"legal_name":"Thread Tutor","contact_line":"thread@example.com","bank_code":"970436","bank_name":"Vietcombank","bank_account_number":"THREAD123","bank_account_holder":"THREAD TUTOR","revision":1,"is_complete":true,"missing_fields":[],"bank_status":"active"}'
    else
      printf '%s\n' '{"legal_name":null,"contact_line":null,"bank_code":null,"bank_name":null,"bank_account_number":null,"bank_account_holder":null,"revision":0,"is_complete":false,"missing_fields":["legal_name","contact_line","bank_code","bank_account_number","bank_account_holder"],"bank_status":"missing"}'
    fi
    ;;
  *"/api/banks"*)
    printf '%s\n' '{"banks":[{"code":"970436","short_name":"Vietcombank","official_name":"Ngân hàng TMCP Ngoại Thương Việt Nam"}]}'
    ;;
  *"/schedule"*)
    printf '%s\n' '{"class":{"class_id":"class-1","schedule_revision":1},"candidate_count":1,"created_count":0,"adopted_count":1,"superseded_count":0,"preserved_count":0}'
    ;;
  *"/sessions/session-1/move"*)
    printf '%s\n' '{"session_id":"session-1","version":3,"state":"active"}'
    ;;
  *"/sessions/session-1/cancel"*)
    printf '%s\n' '{"session_id":"session-1","version":4,"state":"cancelled"}'
    ;;
  *"/sessions/session-1/restore"*)
    printf '%s\n' '{"session_id":"session-1","version":5,"state":"active"}'
    ;;
  *"/roster"*)
    printf '%s\n' '{"class":{"class_id":"class-1","name":"Maths","color":"blue"},"resolved_date":"2026-08-30","students":[{"student_id":"student-1","name":"Thread student","phone":null,"archived":false,"effective_from":"2026-08-30","effective_to":null}]}'
    ;;
  *"<-X> <PUT>"*"/attendance"*)
    printf '%s\n' '{"session_id":"session-1","marked_at":"2026-08-30T03:00:00Z","marks":[{"student_id":"student-1","state":"Present","marked_at":"2026-08-30T03:00:00Z"}]}'
    ;;
  *"/attendance"*)
    printf '%s\n' '{"session":{"session_id":"session-1","class_id":"class-1","class_name":"Maths","class_color":"blue","starts_at":"2026-08-30T03:00:00Z","ends_at":"2026-08-30T04:00:00Z","local_date":"2026-08-30","state":"active"},"eligible":true,"ineligible_reason":null,"revision":"revision-1","students":[{"student_id":"student-1","name":"Thread student","archived":false,"state":null,"marked_at":null}]}'
    ;;
  *"/api/classes"*)
    printf '%s\n' '{"class":{"class_id":"class-1"},"first_session":{"session_id":"session-1","local_date":"2026-08-30"}}'
    ;;
  *"/api/students"*)
    printf '%s\n' '{"student_id":"student-1","name":"Thread student","phone":null}'
    ;;
  *"/api/home"*)
    initial_max=$(( ${CURL_FAILURES:-0} + 1 ))
    if [ "$count" -le "$initial_max" ]; then
      printf '%s\n' '{"local_date":"2026-08-30","setup_defaults":{"local_date":"2026-08-30","start_time":"10:00","end_time":"11:00"},"sessions":[],"billing_projection":{"state":"waiting"}}'
    else
      printf '%s\n' '{"local_date":"2026-08-30","setup_defaults":{"local_date":"2026-08-30","start_time":"10:00","end_time":"11:00"},"sessions":[{"session_id":"session-1","students":[{"student_id":"student-1","attendance_state":"Present"}]}],"billing_projection":{"state":"active"}}'
    fi
    ;;
esac
`)
	writeExecutable(t, directory, "sleep", ":\n")
	return threadFixture{
		path:    directory + string(os.PathListSeparator) + os.Getenv("PATH"),
		callLog: callLog,
		count:   count,
		profile: profile,
	}
}

func assertThreadCalls(t *testing.T, path, tokenTask, gateway string) {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	calls := string(content)
	require.Contains(t, calls, "task <--silent> <"+tokenTask+">\n")
	require.Contains(t, calls, "curl <-fsS> <"+gateway+"/api/home>")
	require.Contains(t, calls, "<"+gateway+"/api/invoice-profile>")
	require.Contains(t, calls, "<"+gateway+"/api/banks>")
	require.Contains(t, calls, "<"+gateway+"/api/classes>")
	require.Contains(t, calls, "<"+gateway+"/api/students>")
	require.Contains(t, calls, "<"+gateway+"/api/classes/class-1/roster>")
	require.Contains(t, calls, "<"+gateway+"/api/classes/class-1/schedule>")
	require.Contains(t, calls, "<"+gateway+"/api/sessions/session-1/move>")
	require.Contains(t, calls, `"start_time": "00:00"`)
	require.Contains(t, calls, `"end_time": "23:59"`)
	require.Contains(t, calls, "<"+gateway+"/api/sessions/session-1/cancel>")
	require.Contains(t, calls, "<"+gateway+"/api/sessions/session-1/restore>")
	require.Contains(t, calls, "<"+gateway+"/api/sessions/session-1/attendance>")
	require.Contains(t, calls, "<-H> <Authorization: Bearer ")
}
