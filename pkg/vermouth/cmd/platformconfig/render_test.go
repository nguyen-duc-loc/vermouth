package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadManifests_ReadsDocumentsAndSkipsEmptyDocuments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rendered.yaml")
	content := "---\nkind: Deployment\nmetadata:\n  name: first\n---\n---\nkind: Service\nmetadata:\n  name: second\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	documents, err := readManifests(path)
	require.NoError(t, err)
	require.Len(t, documents, 2)
	require.Equal(t, "Deployment", stringValue(documents[0], "kind"))
	require.Equal(t, "Service", stringValue(documents[1], "kind"))
}

func TestReadManifests_NamesInvalidInput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invalid.yaml")
	require.NoError(t, os.WriteFile(path, []byte("kind: [\n"), 0o600))

	_, err := readManifests(path)
	require.ErrorContains(t, err, "decode "+path)
}

// covers: AC-9, AC-17
func TestValidateRenderedPolicies_RequiresAnExactTrafficBijection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		googleEnabled bool
		documents     []manifest
		wantError     string
	}{
		{
			name:      "local policies omit the optional Google row",
			documents: []manifest{networkPolicyManifest("gateway-egress", "row-a,row-b")},
		},
		{
			name:          "Google enabled policies include the optional row",
			googleEnabled: true,
			documents: []manifest{
				networkPolicyManifest("gateway-egress", "row-a,row-b"),
				networkPolicyManifest("identity-google-egress", "row-google"),
			},
		},
		{
			name:          "Google row is missing when enabled",
			googleEnabled: true,
			documents:     []manifest{networkPolicyManifest("gateway-egress", "row-a,row-b")},
			wantError:     "rendered traffic row IDs differ",
		},
		{
			name:      "policy annotation changes row order",
			documents: []manifest{networkPolicyManifest("gateway-egress", "row-b,row-a")},
			wantError: "traffic row order",
		},
		{
			name: "policy annotation is missing",
			documents: []manifest{{
				"kind": "NetworkPolicy",
				"metadata": map[string]any{
					"name": "gateway-egress",
				},
			}},
			wantError: "NetworkPolicy \"gateway-egress\"",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateRenderedPolicies(renderedTrafficMatrix(), test.documents, test.googleEnabled)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-10, AC-17
func TestRenderedWorkloadCategory_UsesCommittedCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		kind          string
		component     string
		wantCategory  string
		wantComponent string
	}{
		{name: "gateway application", kind: "Deployment", component: "gateway", wantCategory: workloadGoApplication, wantComponent: "gateway"},
		{name: "web application", kind: "Deployment", component: workloadWeb, wantCategory: workloadWeb, wantComponent: workloadWeb},
		{name: "identity database", kind: "StatefulSet", component: "postgres-identity", wantCategory: workloadPostgres, wantComponent: "postgres-identity"},
		{name: "identity migration", kind: kindJob, component: "migrate-identity", wantCategory: workloadMigration, wantComponent: "migrate-identity"},
		{name: "unrelated Service", kind: "Service", component: "gateway"},
		{name: "unknown component", kind: "Deployment", component: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := manifest{
				"kind": test.kind,
				"metadata": map[string]any{
					"labels": map[string]any{"app.kubernetes.io/component": test.component},
				},
			}

			category, component := renderedWorkloadCategory(document)
			require.Equal(t, test.wantCategory, category)
			require.Equal(t, test.wantComponent, component)
		})
	}
}

// covers: AC-10, AC-15, AC-17
func TestValidateRenderedWorkload_RejectsDriftFromTheWorkloadMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(manifest)
		wantError string
	}{
		{name: "exact workload"},
		{
			name: "replica count",
			mutate: func(document manifest) {
				document["spec"].(map[string]any)["replicas"] = 2
			},
			wantError: "replicas differ",
		},
		{
			name: "ServiceAccount",
			mutate: func(document manifest) {
				podSpecFrom(document)["serviceAccountName"] = "default"
			},
			wantError: "ServiceAccount differs",
		},
		{
			name: "Pod security",
			mutate: func(document manifest) {
				mapValue(podSpecFrom(document), "securityContext")["runAsUser"] = 0
			},
			wantError: "Pod security differs",
		},
		{
			name: "container privilege escalation",
			mutate: func(document manifest) {
				mapValue(containerFrom(document), "securityContext")["allowPrivilegeEscalation"] = true
			},
			wantError: "container security differs",
		},
		{
			name: "container capabilities",
			mutate: func(document manifest) {
				nestedMap(containerFrom(document), "securityContext", "capabilities")["drop"] = []any{"NET_RAW"}
			},
			wantError: "container security differs",
		},
		{
			name: "multiple containers",
			mutate: func(document manifest) {
				containers := sliceValue(podSpecFrom(document), "containers")
				podSpecFrom(document)["containers"] = append(containers, make(map[string]any))
			},
			wantError: "exactly one container",
		},
		{
			name: "CPU request",
			mutate: func(document manifest) {
				nestedMap(containerFrom(document), "resources", "requests")["cpu"] = "20m"
			},
			wantError: "request cpu",
		},
		{
			name: "readiness path",
			mutate: func(document manifest) {
				nestedMap(containerFrom(document), "readinessProbe", "httpGet")["path"] = "/wrong"
			},
			wantError: "readiness probe path differs",
		},
		{
			name: "unexpected liveness probe",
			mutate: func(document manifest) {
				containerFrom(document)["livenessProbe"] = map[string]any{
					"periodSeconds":    5,
					"timeoutSeconds":   2,
					"failureThreshold": 3,
					"httpGet": map[string]any{
						"path": "/health",
					},
				}
			},
			wantError: "unexpected liveness probe",
		},
		{
			name: "writable mount",
			mutate: func(document manifest) {
				containerFrom(document)["volumeMounts"] = []any{}
			},
			wantError: "writable mounts",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, expected := validRenderedWorkloadFixture()
			if test.mutate != nil {
				test.mutate(document)
			}
			err := validateRenderedWorkload(document, "gateway", expected)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-10, AC-11, AC-17
func TestValidateRenderedWorkloadRejectsJobRecoveryDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(manifest)
		wantError string
	}{
		{name: "exact Job recovery contract"},
		{
			name: "active deadline",
			mutate: func(document manifest) {
				document["spec"].(map[string]any)["activeDeadlineSeconds"] = 121
			},
			wantError: "deadline or backoff differs",
		},
		{
			name: "backoff limit",
			mutate: func(document manifest) {
				document["spec"].(map[string]any)["backoffLimit"] = 2
			},
			wantError: "deadline or backoff differs",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, expected := validRenderedWorkloadFixture()
			document["kind"] = kindJob
			document["spec"].(map[string]any)["activeDeadlineSeconds"] = 120
			document["spec"].(map[string]any)["backoffLimit"] = 1
			podSpecFrom(document)["serviceAccountName"] = "migrate-identity"
			expected.Kind = kindJob
			expected.ActiveDeadlineSeconds = 120
			expected.BackoffLimit = 1
			if test.mutate != nil {
				test.mutate(document)
			}

			err := validateRenderedWorkload(document, "migrate-identity", expected)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-10
func TestValidateRenderedWorkload_RequiresExplicitFalseSecurityFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remove func(manifest, *workloadEntry)
	}{
		{
			name: "service account token mount",
			remove: func(document manifest, _ *workloadEntry) {
				delete(podSpecFrom(document), "automountServiceAccountToken")
			},
		},
		{
			name: "privilege escalation",
			remove: func(document manifest, _ *workloadEntry) {
				delete(mapValue(containerFrom(document), "securityContext"), "allowPrivilegeEscalation")
			},
		},
		{
			name: "read only root filesystem",
			remove: func(document manifest, expected *workloadEntry) {
				expected.ReadOnlyRootFilesystem = false
				delete(mapValue(containerFrom(document), "securityContext"), "readOnlyRootFilesystem")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, expected := validRenderedWorkloadFixture()
			test.remove(document, &expected)

			require.Error(t, validateRenderedWorkload(document, "gateway", expected))
		})
	}
}

func networkPolicyManifest(name, rowIDs string) manifest {
	return manifest{
		"kind": "NetworkPolicy",
		"metadata": map[string]any{
			"name": name,
			"annotations": map[string]any{
				"vermouth.dev/traffic-row-ids": rowIDs,
			},
		},
	}
}

func renderedTrafficMatrix() trafficMatrix {
	return trafficMatrix{
		Rows: []trafficRow{
			{ID: "row-a", PolicyName: "gateway-egress", Environment: "all"},
			{ID: "row-b", PolicyName: "gateway-egress", Environment: "all"},
			{ID: "row-google", PolicyName: "identity-google-egress", Environment: environmentGoogleEnabled},
		},
	}
}

func validRenderedWorkloadFixture() (manifest, workloadEntry) {
	expected := workloadEntry{
		Kind:                   "Deployment",
		Replicas:               1,
		ServiceAccount:         "component",
		RunAsUser:              10001,
		RunAsGroup:             10001,
		ReadOnlyRootFilesystem: true,
		Readiness: &workloadProbe{
			Type:             "http",
			Path:             "/ready",
			PeriodSeconds:    5,
			TimeoutSeconds:   2,
			FailureThreshold: 3,
		},
		WritableMounts: []writableMount{
			{Name: "cache", MountPath: "/tmp", Medium: "Disk", SizeLimit: "64Mi"},
		},
	}
	expected.Resources.Requests = map[string]string{"cpu": "10m", "memory": "32Mi"}
	expected.Resources.Limits = map[string]string{"cpu": "100m", "memory": "128Mi"}

	document := manifest{
		"kind": "Deployment",
		"metadata": map[string]any{
			"labels": map[string]any{"app.kubernetes.io/component": "gateway"},
		},
		"spec": map[string]any{
			"replicas": 1,
			"template": map[string]any{
				"spec": map[string]any{
					"serviceAccountName":           "gateway",
					"automountServiceAccountToken": false,
					"securityContext": map[string]any{
						"runAsNonRoot": true,
						"runAsUser":    10001,
						"runAsGroup":   10001,
						"seccompProfile": map[string]any{
							"type": "RuntimeDefault",
						},
					},
					"containers": []any{
						map[string]any{
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"readOnlyRootFilesystem":   true,
								"capabilities": map[string]any{
									"drop": []any{"ALL"},
								},
							},
							"resources": map[string]any{
								"requests": map[string]any{"cpu": "10m", "memory": "32Mi"},
								"limits":   map[string]any{"cpu": "100m", "memory": "128Mi"},
							},
							"readinessProbe": map[string]any{
								"periodSeconds":    5,
								"timeoutSeconds":   2,
								"failureThreshold": 3,
								"httpGet": map[string]any{
									"path": "/ready",
								},
							},
							"volumeMounts": []any{
								map[string]any{"name": "cache", "mountPath": "/tmp", "readOnly": false},
							},
						},
					},
					"volumes": []any{
						map[string]any{
							"name": "cache",
							"emptyDir": map[string]any{
								"sizeLimit": "64Mi",
							},
						},
					},
				},
			},
		},
	}
	return document, expected
}

func podSpecFrom(document manifest) map[string]any {
	return nestedMap(document, "spec", "template", "spec")
}

func containerFrom(document manifest) map[string]any {
	container, _ := sliceValue(podSpecFrom(document), "containers")[0].(map[string]any)
	return container
}
