package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-11
func TestValidateDocument_AcceptsGeneratedYAMLNextFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	schemaPath := filepath.Join(directory, "values.schema.json")
	documentPath := filepath.Join(directory, "runtime-values.yaml.next")
	require.NoError(t, os.WriteFile(schemaPath, []byte(`{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["schemaVersion"],
  "properties": {"schemaVersion": {"const": 1}}
}`), 0o600))
	require.NoError(t, os.WriteFile(documentPath, []byte("schemaVersion: 1\n"), 0o600))

	require.NoError(t, validateDocument(schemaPath, documentPath))
}

// covers: AC-11, AC-17
func TestValidateDocument_RejectsAdditionalYAMLDocuments(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	schemaPath := filepath.Join(directory, "values.schema.json")
	documentPath := filepath.Join(directory, "runtime-values.yaml.next")
	require.NoError(t, os.WriteFile(schemaPath, []byte(`{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["schemaVersion"],
  "properties": {"schemaVersion": {"const": 1}}
}`), 0o600))
	require.NoError(t, os.WriteFile(
		documentPath,
		[]byte("schemaVersion: 1\n---\nschemaVersion: 1\n"),
		0o600,
	))

	err := validateDocument(schemaPath, documentPath)
	require.ErrorContains(t, err, "expected one YAML document")
}

// covers: AC-1, AC-9, AC-10, AC-11, AC-17
func TestValidateInputs_RepositoryContract(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	require.NoError(t, err)
	require.NoError(t, validateInputs(root))
}

// covers: AC-1, AC-17
func TestValidateVersions_RegistryIdentity(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	require.NoError(t, err)
	config, err := strictYAML[versionsConfig](
		filepath.Join(root, "deploy", "platform", "versions.yaml"),
	)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*versionsConfig)
	}{
		{
			name: "prefixed cluster endpoint",
			mutate: func(config *versionsConfig) {
				config.Registry.ClusterEndpoint = "k3d-vermouth-registry:5000"
			},
		},
		{
			name: "wildcard host bind",
			mutate: func(config *versionsConfig) {
				config.Bindings.RegistryHost = "0.0.0.0"
			},
		},
		{
			name: "privileged application port",
			mutate: func(config *versionsConfig) {
				config.Bindings.ApplicationPort = 80
			},
		},
		{
			name: "mutable registry name",
			mutate: func(config *versionsConfig) {
				config.Registry.ContainerName = "k3d-vermouth-registry"
			},
		},
		{
			name: "reversed builder platforms",
			mutate: func(config *versionsConfig) {
				config.Builder.Platforms = []string{platformAMD64, platformARM64}
			},
		},
		{
			name: "missing immutable identity fields",
			mutate: func(config *versionsConfig) {
				config.ImmutableIdentityFields = nil
			},
		},
		{
			name: "missing cache cleanup deadline",
			mutate: func(config *versionsConfig) {
				config.Deadlines.CacheCleanSeconds = 0
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed := config
			test.mutate(&changed)
			require.Error(t, validateVersions(changed))
		})
	}
}

// covers: AC-1, AC-17
func TestStrictYAML_RejectsUnknownFieldsAndExtraDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantError string
	}{
		{
			name:      "unknown field",
			content:   "schemaVersion: 1\nunknownField: true\n",
			wantError: "field unknownField not found",
		},
		{
			name:      "second document",
			content:   "schemaVersion: 1\n---\nschemaVersion: 1\n",
			wantError: "expected one YAML document",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "versions.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.content), 0o600))

			_, err := strictYAML[versionsConfig](path)
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-2, AC-7, AC-17
func TestValidateDeclaredInputs_RejectsUnsafeOrAmbiguousPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	root := filepath.Join(workspace, "repo")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.txt"), []byte("first"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "inside.txt"), []byte("inside"), 0o600))
	outsidePath := filepath.Join(workspace, "outside.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("outside"), 0o600))

	tests := []struct {
		name      string
		inputs    []string
		wantError string
	}{
		{name: "separate inputs", inputs: []string{"first.txt", "nested"}},
		{name: "empty input set", inputs: nil, wantError: "at least one input"},
		{name: "absolute path", inputs: []string{outsidePath}, wantError: "outside the repository"},
		{name: "parent path", inputs: []string{"../outside.txt"}, wantError: "outside the repository"},
		{name: "missing path", inputs: []string{"missing.txt"}, wantError: "no such file"},
		{
			name:      "overlapping directory and file",
			inputs:    []string{"nested", "nested/inside.txt"},
			wantError: "overlaps",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateDeclaredInputs(root, test.inputs)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-2, AC-7, AC-8, AC-17
func TestValidateImageLockRejectsMutableOrIncompleteImageIdentities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*imageLock)
		wantError string
	}{
		{name: "complete image lock"},
		{
			name: "changed probe hash",
			mutate: func(lock *imageLock) {
				lock.Probe.InputSHA256 = strings.Repeat("0", 64)
			},
			wantError: "platform probe identity differs",
		},
		{
			name: "external image without an immutable digest",
			mutate: func(lock *imageLock) {
				external := lock.External["scratch"]
				external.Digest = "latest"
				lock.External["scratch"] = external
			},
			wantError: "invalid digest",
		},
		{
			name: "external image with reversed platforms",
			mutate: func(lock *imageLock) {
				external := lock.External["scratch"]
				external.Platforms = []string{platformAMD64, platformARM64}
				lock.External["scratch"] = external
			},
			wantError: "platforms must be arm64 then amd64",
		},
		{
			name: "built key differs from workload",
			mutate: func(lock *imageLock) {
				image := lock.Built["gateway"]
				image.Workload = "identity"
				lock.Built["gateway"] = image
			},
			wantError: "does not match workload",
		},
		{
			name: "built repository differs from workload",
			mutate: func(lock *imageLock) {
				image := lock.Built["gateway"]
				image.RepositoryPath = "personal/gateway"
				lock.Built["gateway"] = image
			},
			wantError: "repositoryPath",
		},
		{
			name: "built image names an unknown base",
			mutate: func(lock *imageLock) {
				image := lock.Built["gateway"]
				image.BaseLocks = []string{"unknown"}
				lock.Built["gateway"] = image
			},
			wantError: "unknown base lock",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root, lock := validImageLockFixture(t)
			if test.mutate != nil {
				test.mutate(&lock)
			}

			err := validateImageLock(root, lock)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-2, AC-11
func TestValidateSecretInventory_RejectsIncompleteContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*secretInventory)
		wantError string
	}{
		{name: "complete inventory"},
		{
			name: "wrong hash schema",
			mutate: func(inventory *secretInventory) {
				inventory.HashSchema = "plain-sha256"
			},
			wantError: "hashSchema vermouth-secret-v1",
		},
		{
			name: "missing foundation inventory",
			mutate: func(inventory *secretInventory) {
				inventory.Foundation = nil
			},
			wantError: "must not be empty",
		},
		{
			name: "runtime row without consumers",
			mutate: func(inventory *secretInventory) {
				entry := inventory.Runtime["identity-db"]
				entry.Consumers = nil
				inventory.Runtime["identity-db"] = entry
			},
			wantError: "runtime row \"identity-db\" is incomplete",
		},
		{
			name: "database row with wrong port",
			mutate: func(inventory *secretInventory) {
				entry := inventory.Runtime["identity-db"]
				entry.Database.Port = 5433
				inventory.Runtime["identity-db"] = entry
			},
			wantError: "invalid database contract",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inventory := validSecretInventory()
			if test.mutate != nil {
				test.mutate(&inventory)
			}
			err := validateSecretInventory(inventory)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-9, AC-17
func TestValidateTrafficMatrix_RejectsAmbiguousRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*trafficMatrix)
		wantError string
	}{
		{name: "complete matrix"},
		{
			name: "duplicate row id",
			mutate: func(matrix *trafficMatrix) {
				matrix.Rows[1].ID = matrix.Rows[0].ID
			},
			wantError: "duplicate row id",
		},
		{
			name: "duplicate row order",
			mutate: func(matrix *trafficMatrix) {
				matrix.Rows[1].Order = matrix.Rows[0].Order
			},
			wantError: "duplicate row order",
		},
		{
			name: "invalid direction",
			mutate: func(matrix *trafficMatrix) {
				matrix.Rows[0].Direction = "Sideways"
			},
			wantError: "invalid direction",
		},
		{
			name: "inconsistent shared policy",
			mutate: func(matrix *trafficMatrix) {
				matrix.Rows[1].PodSelector = map[string]string{"app.kubernetes.io/component": "identity"}
			},
			wantError: "inconsistent target or direction",
		},
		{
			name: "unknown selector key",
			mutate: func(matrix *trafficMatrix) {
				matrix.Rows[0].PodSelector = map[string]string{"unsafe.example/selector": "gateway"}
			},
			wantError: "unknown selector key",
		},
		{
			name: "invalid named port",
			mutate: func(matrix *trafficMatrix) {
				matrix.Rows[0].Ports[0].Port = 0
			},
			wantError: "invalid named port",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			matrix := validTrafficMatrix()
			if test.mutate != nil {
				test.mutate(&matrix)
			}
			err := validateTrafficMatrix(matrix)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-10, AC-15, AC-17
func TestValidateWorkloadMatrix_RequiresEveryResourceContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*workloadMatrix)
		wantError string
	}{
		{name: "complete matrix"},
		{
			name: "missing workload",
			mutate: func(matrix *workloadMatrix) {
				delete(matrix.Workloads, workloadGarageInit)
			},
			wantError: "workload \"garage-init\" is missing",
		},
		{
			name: "multiple replicas",
			mutate: func(matrix *workloadMatrix) {
				entry := matrix.Workloads[workloadDevelopmentToken]
				entry.Replicas = 2
				matrix.Workloads[workloadDevelopmentToken] = entry
			},
			wantError: "invalid replicas",
		},
		{
			name: "missing memory limit",
			mutate: func(matrix *workloadMatrix) {
				entry := matrix.Workloads[workloadWeb]
				delete(entry.Resources.Limits, "memory")
				matrix.Workloads[workloadWeb] = entry
			},
			wantError: "CPU and memory requests and limits",
		},
		{
			name: "wrong resource names with the expected count",
			mutate: func(matrix *workloadMatrix) {
				entry := matrix.Workloads[workloadWeb]
				entry.Resources.Requests = map[string]string{
					"ephemeral-storage": "32Mi",
					"storage":           "64Mi",
				}
				entry.Resources.Limits = map[string]string{
					"ephemeral-storage": "64Mi",
					"storage":           "128Mi",
				}
				matrix.Workloads[workloadWeb] = entry
			},
			wantError: "CPU and memory requests and limits",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			matrix := validWorkloadMatrix()
			if test.mutate != nil {
				test.mutate(&matrix)
			}
			err := validateWorkloadMatrix(matrix)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestGetValue_ReturnsOnlyTheSelectedValue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "values.yaml")
	require.NoError(t, os.WriteFile(path, []byte("root:\n  child: value\nitems:\n  - one\n"), 0o600))
	tests := []struct {
		name      string
		wanted    string
		want      string
		wantError string
	}{
		{name: "nested scalar", wanted: "root.child", want: "value\n"},
		{name: "collection", wanted: "items", want: "[\"one\"]\n"},
		{name: "absent path", wanted: "root.missing", wantError: "is absent"},
		{name: "path through scalar", wanted: "root.child.missing", wantError: "does not select an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			err := getValue(path, test.wanted, &output)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, output.String())
		})
	}
}

// covers: AC-1, AC-17
func TestGetValue_RejectsAdditionalYAMLDocuments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "values.yaml")
	require.NoError(t, os.WriteFile(
		path,
		[]byte("root:\n  child: value\n---\nroot:\n  child: replacement\n"),
		0o600,
	))

	var output bytes.Buffer
	err := getValue(path, "root.child", &output)
	require.ErrorContains(t, err, "expected one YAML document")
	require.Empty(t, output.String())
}

func validSecretInventory() secretInventory {
	database := &databaseInventory{
		ServiceDNS:     "postgres-identity",
		Port:           5432,
		Database:       "identity",
		Role:           "vermouth_identity",
		PasswordSecret: "identity-db",
		PasswordKey:    "password",
		URLTemplate:    "postgres://{role}:{password}@{serviceDNS}:{port}/{database}?sslmode=disable",
	}
	return secretInventory{
		SchemaVersion: schemaVersion,
		HashSchema:    "vermouth-secret-v1",
		Foundation: map[string]secretEntry{
			"garage": {
				NameBase: "garage",
				Keys: map[string]inventoryKey{
					"token": {Source: "env", Generation: "random"},
				},
				Consumers: []string{"garage"},
			},
		},
		Runtime: map[string]secretEntry{
			"identity-db": {
				NameBase: "identity-db",
				Database: database,
				Keys: map[string]inventoryKey{
					"password": {Source: "env", Generation: "random"},
				},
				Consumers: []string{"identity", "migrate-identity"},
			},
		},
	}
}

func validImageLockFixture(t *testing.T) (string, imageLock) {
	t.Helper()

	root := t.TempDir()
	probeContent := []byte("FROM scratch\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "probe.Dockerfile"), probeContent, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "input.txt"), []byte("input\n"), 0o600))
	probeHash := sha256.Sum256(probeContent)

	lock := imageLock{
		SchemaVersion: schemaVersion,
		External: map[string]externalImage{
			"scratch": {
				Image:     "scratch",
				Digest:    "sha256:" + strings.Repeat("a", 64),
				Platforms: []string{platformARM64, platformAMD64},
			},
		},
		Built: map[string]builtImage{
			"gateway": {
				Workload:          "gateway",
				RepositoryPath:    "vermouth/gateway",
				Dockerfile:        "Dockerfile",
				Target:            "runtime",
				NativePlatforms:   []string{platformARM64},
				MultiPlatforms:    []string{platformARM64, platformAMD64},
				Inputs:            []string{"input.txt"},
				BaseLocks:         []string{"scratch"},
				HashSchema:        "vermouth-image-v1",
				NativeTagTemplate: "dev-{hash12}",
				MultiTagTemplate:  "multi-{hash12}",
				ChildTagTemplate:  "multi-{hash12}-{architecture}",
			},
		},
	}
	lock.Probe.Dockerfile = "probe.Dockerfile"
	lock.Probe.InputSHA256 = hex.EncodeToString(probeHash[:])
	lock.Probe.Platform = platformARM64
	lock.Probe.RepositoryPath = "vermouth/platform-probe"
	lock.Probe.BaseLocks = []string{"scratch"}
	return root, lock
}

func validTrafficMatrix() trafficMatrix {
	return trafficMatrix{
		SchemaVersion: schemaVersion,
		Rows: []trafficRow{
			{
				ID:          "gateway-to-identity",
				PolicyName:  "gateway-egress",
				Direction:   "Egress",
				Environment: "all",
				Order:       10,
				PodSelector: map[string]string{"app.kubernetes.io/component": "gateway"},
				Ports:       []matrixPort{{Name: "http", Protocol: "TCP", Port: 8080}},
			},
			{
				ID:          "gateway-to-teaching",
				PolicyName:  "gateway-egress",
				Direction:   "Egress",
				Environment: "all",
				Order:       20,
				PodSelector: map[string]string{"app.kubernetes.io/component": "gateway"},
				Ports:       []matrixPort{{Name: "http", Protocol: "TCP", Port: 8080}},
			},
		},
	}
}

func validWorkloadMatrix() workloadMatrix {
	names := []string{
		workloadGoApplication,
		workloadWeb,
		workloadPostgres,
		workloadRedpanda,
		workloadGarage,
		workloadMigration,
		workloadGarageInit,
		workloadDevelopmentToken,
		"traefik",
	}
	workloads := make(map[string]workloadEntry, len(names))
	for _, name := range names {
		entry := workloadEntry{
			Kind:           "Deployment",
			Replicas:       1,
			ServiceAccount: "component",
		}
		entry.Resources.Requests = map[string]string{"cpu": "10m", "memory": "32Mi"}
		entry.Resources.Limits = map[string]string{"cpu": "100m", "memory": "128Mi"}
		workloads[name] = entry
	}
	return workloadMatrix{SchemaVersion: schemaVersion, Workloads: workloads}
}
