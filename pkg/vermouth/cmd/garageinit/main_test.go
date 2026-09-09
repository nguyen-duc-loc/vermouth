package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	urlpkg "net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRun_ReconcilesAnEmptyGarage(t *testing.T) {
	t.Parallel()
	const (
		nodeID    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		accessKey = "GK0123456789abcdef01234567"
	)

	state := struct {
		role       *nodeRole
		staged     []roleChange
		bucket     bucketInfo
		keys       []keySummary
		permission bucketPermission
	}{staged: []roleChange{}, keys: []keySummary{}}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v2/GetClusterStatus":
			writeTestJSON(t, response, clusterStatus{
				LayoutVersion: 0,
				Nodes:         []clusterNode{{ID: nodeID, Role: state.role}},
			})
		case "/v2/GetClusterLayout":
			writeTestJSON(t, response, clusterLayout{Version: 0, Roles: []roleChange{}, StagedRoleChanges: state.staged})
		case "/v2/UpdateClusterLayout":
			var body struct {
				Roles []roleChange `json:"roles"`
			}
			decodeTestJSON(t, request, &body)
			state.staged = body.Roles
			writeTestJSON(t, response, clusterLayout{Version: 0, Roles: []roleChange{}, StagedRoleChanges: state.staged})
		case "/v2/ApplyClusterLayout":
			role := roleFromChange(state.staged[0])
			state.role = &role
			state.staged = []roleChange{}
			writeTestJSON(t, response, map[string]any{"message": []string{}, "layout": clusterLayout{Version: 1}})
		case "/v2/GetBucketInfo":
			if state.bucket.ID == "" {
				response.WriteHeader(http.StatusNotFound)
				return
			}
			bucket := state.bucket
			if state.permission.Read || state.permission.Write || state.permission.Owner {
				bucket.Keys = []bucketKey{{AccessKeyID: accessKey, Name: "billing", Permissions: state.permission}}
			}
			writeTestJSON(t, response, bucket)
		case "/v2/CreateBucket":
			state.bucket = bucketInfo{ID: "bucket-id", GlobalAliases: []string{"vermouth-invoices"}, Keys: []bucketKey{}}
			writeTestJSON(t, response, state.bucket)
		case "/v2/ListKeys":
			writeTestJSON(t, response, state.keys)
		case "/v2/ImportKey":
			state.keys = []keySummary{{ID: accessKey, Name: "billing"}}
			writeTestJSON(t, response, keyInfo{AccessKeyID: accessKey, Name: "billing"})
		case "/v2/GetKeyInfo":
			writeTestJSON(t, response, keyInfo{AccessKeyID: accessKey, Name: "billing"})
		case "/v2/AllowBucketKey":
			state.permission = bucketPermission{Read: true, Write: true}
			writeTestJSON(t, response, state.bucket)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	endpoint, err := urlpkg.Parse(server.URL)
	require.NoError(t, err)
	client := &adminClient{httpClient: server.Client(), endpoint: endpoint, token: "test-token"}
	cfg := config{
		endpoint:  endpoint,
		token:     "test-token",
		zone:      "local",
		bucket:    "vermouth-invoices",
		keyName:   "billing",
		accessKey: accessKey,
		secretKey: "test-secret",
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	require.NoError(t, run(ctx, client, cfg))
	require.NotNil(t, state.role)
	require.Equal(t, desiredCapacity, *state.role.Capacity)
	require.Equal(t, bucketPermission{Read: true, Write: true}, state.permission)
}

// covers: AC-11, AC-16
func TestRunAcceptsAnAlreadyReconciledGarageWithoutMutation(t *testing.T) {
	t.Parallel()

	const (
		nodeID    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		accessKey = "GK0123456789abcdef01234567"
	)
	capacity := desiredCapacity
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost {
			postCount++
		}
		switch request.URL.Path {
		case "/v2/GetClusterStatus":
			writeTestJSON(t, response, clusterStatus{Nodes: []clusterNode{{
				ID: nodeID,
				Role: &nodeRole{
					Zone: "local", Capacity: &capacity, Tags: []string{},
				},
			}}})
		case "/v2/GetBucketInfo":
			writeTestJSON(t, response, bucketInfo{
				ID:            "bucket-id",
				GlobalAliases: []string{"vermouth-invoices"},
				Keys: []bucketKey{{
					AccessKeyID: accessKey,
					Name:        "billing",
					Permissions: bucketPermission{Read: true, Write: true},
				}},
			})
		case "/v2/ListKeys":
			writeTestJSON(t, response, []keySummary{{ID: accessKey, Name: "billing"}})
		case "/v2/GetKeyInfo":
			writeTestJSON(t, response, keyInfo{AccessKeyID: accessKey, Name: "billing"})
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	endpoint, err := urlpkg.Parse(server.URL)
	require.NoError(t, err)
	client := &adminClient{httpClient: server.Client(), endpoint: endpoint, token: "test-token"}
	cfg := config{
		endpoint: endpoint, token: "test-token", zone: "local", bucket: "vermouth-invoices",
		keyName: "billing", accessKey: accessKey, secretKey: "test-secret",
	}

	require.NoError(t, run(t.Context(), client, cfg))
	require.Zero(t, postCount)
}

// covers: AC-11, AC-16
func TestLoadConfigRejectsMissingOrDriftedGarageIdentity(t *testing.T) {
	t.Setenv("GARAGE_ADMIN_ENDPOINT", "http://garage:3903")
	t.Setenv("GARAGE_ADMIN_TOKEN", strings.Repeat("a", 64))
	t.Setenv("GARAGE_ZONE", "local")
	t.Setenv("GARAGE_BUCKET", "vermouth-invoices")
	t.Setenv("GARAGE_KEY_NAME", "billing")
	t.Setenv("GARAGE_ACCESS_KEY_ID", "GK0123456789abcdef01234567")
	t.Setenv("GARAGE_SECRET_ACCESS_KEY", strings.Repeat("b", 64))

	tests := []struct {
		name      string
		variable  string
		value     string
		wantError string
	}{
		{name: "missing admin token", variable: "GARAGE_ADMIN_TOKEN", value: " ", wantError: "GARAGE_ADMIN_TOKEN is required"},
		{name: "admin endpoint path", variable: "GARAGE_ADMIN_ENDPOINT", value: "http://garage:3903/admin", wantError: "must be exactly"},
		{name: "wrong zone", variable: "GARAGE_ZONE", value: "remote", wantError: "differ from the local contract"},
		{name: "wrong bucket", variable: "GARAGE_BUCKET", value: "other", wantError: "differ from the local contract"},
		{name: "wrong key name", variable: "GARAGE_KEY_NAME", value: "owner", wantError: "differ from the local contract"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.variable, test.value)

			_, err := loadConfig()
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func writeTestJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	require.NoError(t, json.NewEncoder(response).Encode(value))
}

func decodeTestJSON(t *testing.T, request *http.Request, value any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(request.Body).Decode(value))
}
