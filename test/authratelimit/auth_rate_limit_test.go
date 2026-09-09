// Package authratelimit_test proves the public gateway refuses authentication
// floods before identity can grow its pending sign in state (spec 0007).
//
//nolint:paralleltest // The one integration test owns shared identity database state.
package authratelimit_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const physicalPendingAttemptBound = 666

func TestAuthRateLimit_RefusesBeforeIdentityStateGrows(t *testing.T) {
	if runIdentity := os.Getenv("VERMOUTH_AUTH_RATE_LIMIT_TEST_RUN"); runIdentity != "" {
		t.Logf("evidence target invocation %s", runIdentity)
	}
	databaseURL := os.Getenv("IDENTITY_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("IDENTITY_DATABASE_URL is not set, run task test:auth-rate-limit")
	}
	root := repositoryRoot(t)
	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(t.Context()))
	t.Cleanup(pool.Close)
	_, err = pool.Exec(t.Context(), "TRUNCATE TABLE login_attempts")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupContext, "TRUNCATE TABLE login_attempts")
	})

	identityAddress := freeAddress(t)
	gatewayAddress := freeAddress(t)
	gatewayOrigin := localhostOrigin(t, gatewayAddress)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	kid := "auth-rate-limit-test"
	publicKeys := kid + ":" + base64.StdEncoding.EncodeToString(publicKey)
	binaries := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(binaries, 0o755))
	identityBinary := filepath.Join(binaries, "identity")
	gatewayBinary := filepath.Join(binaries, "gateway")
	buildBinary(t, root, identityBinary, "./services/identity/cmd/identity")
	buildBinary(t, root, gatewayBinary, "./gateway/cmd/gateway")

	identity := startProcess(t, root, identityBinary, map[string]string{
		"LOG_LEVEL":                     "error",
		"BROKER_SEEDS":                  "localhost:19092",
		"TOPIC_PARTITIONS":              "3",
		"IDENTITY_HTTP_ADDR":            identityAddress,
		"IDENTITY_DATABASE_URL":         databaseURL,
		"IDENTITY_TOKEN_KID":            kid,
		"IDENTITY_TOKEN_PRIVATE_KEY":    base64.StdEncoding.EncodeToString(privateKey),
		"TOKEN_PUBLIC_KEYS":             publicKeys,
		"VERMOUTH_GOOGLE_AUTH_ENABLED":  "true",
		"IDENTITY_GOOGLE_CLIENT_ID":     "dummy-client-id",
		"IDENTITY_GOOGLE_CLIENT_SECRET": "dummy-client-secret",
		"IDENTITY_GOOGLE_REDIRECT_URL":  gatewayOrigin + "/api/auth/google/callback",
		"IDENTITY_APP_URL":              gatewayOrigin,
		"IDENTITY_SIGNUP_ALLOWLIST":     "",
		"IDENTITY_COOKIE_SECURE":        "false",
		"IDENTITY_SWEEP_INTERVAL":       "10m",
	})
	waitForHTTP(t, identity, "http://"+identityAddress+"/health")

	gateway := startProcess(t, root, gatewayBinary, map[string]string{
		"LOG_LEVEL":                         "error",
		"GATEWAY_HTTP_ADDR":                 gatewayAddress,
		"TOKEN_PUBLIC_KEYS":                 publicKeys,
		"GATEWAY_IDENTITY_URL":              "http://" + identityAddress,
		"GATEWAY_TEACHING_URL":              "http://" + identityAddress,
		"GATEWAY_BILLING_URL":               "http://" + identityAddress,
		"GATEWAY_NOTIFICATIONS_URL":         "http://" + identityAddress,
		"GATEWAY_TRUSTED_PROXY_CIDRS":       "127.0.0.0/8",
		"GATEWAY_AUTH_RATE_START_IP":        "1/1m,20/1h",
		"GATEWAY_AUTH_RATE_START_GLOBAL":    "3/1m,500/1h",
		"GATEWAY_AUTH_RATE_CALLBACK_IP":     "10/1m,60/1h",
		"GATEWAY_AUTH_RATE_CALLBACK_GLOBAL": "200/1m,1000/1h",
		"GATEWAY_AUTH_RATE_REFRESH_IP":      "30/1m,120/1h",
		"GATEWAY_AUTH_RATE_REFRESH_TOKEN":   "10/1m,120/1h",
		"GATEWAY_AUTH_RATE_REFRESH_GLOBAL":  "300/1m,3000/1h",
	})
	waitForHTTP(t, gateway, "http://"+gatewayAddress+"/health")

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	startURL := "http://" + gatewayAddress +
		"/api/auth/google/start?tz=Asia%2FHo_Chi_Minh&lang=vi&redirect_to=%2F"

	allowed := startRequest(t, client, startURL, "198.51.100.8")
	require.Equal(t, http.StatusFound, allowed.StatusCode)
	require.Contains(t, allowed.Header.Get("Location"), "accounts.google.com")
	closeResponse(t, allowed)
	require.Equal(t, int64(1), pendingAttemptCount(t, pool))

	callerLimited := startRequest(t, client, startURL, "198.51.100.8")
	require.Equal(t, http.StatusFound, callerLimited.StatusCode)
	require.Equal(t, "/signin?error=rate_limited", callerLimited.Header.Get("Location"))
	require.Equal(t, "60", callerLimited.Header.Get("Retry-After"))
	closeResponse(t, callerLimited)
	require.Equal(t, int64(1), pendingAttemptCount(t, pool))

	for _, address := range []string{"198.51.100.9", "198.51.100.10"} {
		response := startRequest(t, client, startURL, address)
		require.Equal(t, http.StatusFound, response.StatusCode)
		require.Contains(t, response.Header.Get("Location"), "accounts.google.com")
		closeResponse(t, response)
	}
	globalLimited := startRequest(t, client, startURL, "198.51.100.11")
	require.Equal(t, http.StatusFound, globalLimited.StatusCode)
	require.Equal(t, "/signin?error=rate_limited", globalLimited.Header.Get("Location"))
	closeResponse(t, globalLimited)

	pending := pendingAttemptCount(t, pool)
	require.Equal(t, int64(3), pending)
	require.LessOrEqual(t, pending, int64(physicalPendingAttemptBound))
}

type runningProcess struct {
	command *exec.Cmd
	done    chan error
	logPath string
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	require.NoError(t, err)
	for {
		_, statErr := os.Stat(filepath.Join(directory, "go.work"))
		if statErr == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		require.NotEqual(t, directory, parent, "could not find repository root")
		directory = parent
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

func localhostOrigin(t *testing.T, address string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	return "http://localhost:" + port
}

func buildBinary(t *testing.T, root, output, packagePath string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "go", "build", "-o", output, packagePath)
	command.Dir = root
	combined, err := command.CombinedOutput()
	require.NoError(t, err, string(combined))
}

func startProcess(
	t *testing.T,
	root string,
	binary string,
	overrides map[string]string,
) *runningProcess {
	t.Helper()
	logFile, err := os.CreateTemp(t.TempDir(), "service-*.log")
	require.NoError(t, err)
	command := exec.CommandContext(context.WithoutCancel(t.Context()), binary)
	command.Dir = root
	command.Env = environmentWith(overrides)
	command.Stdout = logFile
	command.Stderr = logFile
	require.NoError(t, command.Start())
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	process := &runningProcess{command: command, done: done, logPath: logFile.Name()}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Signal(os.Interrupt)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			<-done
		}
		_ = logFile.Close()
	})
	return process
}

func environmentWith(overrides map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if _, replaced := overrides[name]; !replaced {
			environment = append(environment, value)
		}
	}
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		environment = append(environment, name+"="+overrides[name])
	}
	return environment
}

func waitForHTTP(t *testing.T, process *runningProcess, target string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-process.done:
			process.done <- err
			logBytes, _ := os.ReadFile(process.logPath)
			require.NoError(t, err, string(logBytes))
			t.Fatalf("process exited before %s became ready: %s", target, logBytes)
		default:
		}
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
		if requestErr != nil {
			t.Fatalf("build readiness request: %v", requestErr)
		}
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	logBytes, _ := os.ReadFile(process.logPath)
	t.Fatalf("timed out waiting for %s: %s", target, logBytes)
}

func startRequest(t *testing.T, client *http.Client, target, forwardedFor string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	require.NoError(t, err)
	request.Header.Set("X-Forwarded-For", forwardedFor)
	response, err := client.Do(request)
	require.NoError(t, err)
	return response
}

func closeResponse(t *testing.T, response *http.Response) {
	t.Helper()
	_, err := io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
}

func pendingAttemptCount(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var count int64
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM login_attempts").Scan(&count))
	return count
}
