package platform_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-2, AC-8
func TestProductionConfigurationPinsTheExactTarget(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "prod-common.sh")},
		"prod_init\n",
		"",
		productionEnvironment(t),
	)

	require.NoError(t, result.err, result.stderr)
}

// covers: AC-4, AC-10
func TestProductionWorkflowInstallsTheAuthEvidenceToolchain(t *testing.T) {
	t.Parallel()

	workflow, err := os.ReadFile(repoFile(t, ".github", "workflows", "production.yml"))
	require.NoError(t, err)

	for _, required := range []string{
		"uses: pnpm/action-setup@v6",
		"uses: actions/setup-node@v7",
		"node-version: '24.19'",
		"go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0",
	} {
		require.Contains(t, string(workflow), required)
	}
}

// covers: AC-4
func TestProductionPublisherAssemblesEveryVerifiedImageRecord(t *testing.T) {
	t.Parallel()

	publisher, err := os.ReadFile(repoFile(t, "deploy", "production", "publish-images.sh"))
	require.NoError(t, err)

	source := string(publisher)
	publication := strings.Index(source, "for workload in $production_workloads; do\n  publish_image")
	assembly := strings.Index(source, "records=$work/verified-records.jsonl")
	require.Positive(t, publication)
	require.Greater(t, assembly, publication)
	require.Contains(t, source[:assembly], "verify_image \"$plan\" \"$reference\" >\"$record\"")
	require.NotContains(t, source, "record=$3")
	require.Contains(t, source[:assembly], "jq -cn \\")
	require.Contains(t, source[assembly:], "[ -s \"$record\" ]")
	require.Contains(t, source[assembly:], "cat \"$record\" >>\"$records\"")
	require.NotContains(t, source[:assembly], ">>\"$records\"")
}

// covers: AC-13
func TestMigrationCompatibilityResolvesTheDeploymentBaseBeforeChangingDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	base := filepath.Join(directory, "deployment-base.json")
	require.NoError(t, os.WriteFile(base, []byte("{}\n"), 0o600))

	tools := t.TempDir()
	writeExecutable(t, tools, "go", "printf '%s\\n' \"$*\"\nexit 17\n")
	relativeBase, err := filepath.Rel(repoFile(t), base)
	require.NoError(t, err)
	result := runCommand(
		t,
		"",
		map[string]string{"PATH": tools + string(os.PathListSeparator) + os.Getenv("PATH")},
		[]string{repoFile(t, "test", "migrationcompat", "run.sh"), relativeBase},
	)

	require.Error(t, result.err)
	arguments := strings.Fields(result.stdout)
	require.NotEmpty(t, arguments, result.stderr)
	require.Equal(t, base, arguments[len(arguments)-1])
}

// covers: AC-1, AC-2
func TestProductionConfigurationRejectsAnotherHost(t *testing.T) {
	t.Parallel()

	environment := productionEnvironment(t)
	environment["PROD_HOST"] = "203.0.113.10"
	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "prod-common.sh")},
		"prod_init\n",
		"",
		environment,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "PROD_HOST must be 4.194.251.123")
}

// covers: AC-1, AC-5, AC-8
func TestProductionConfigurationRejectsMultilineRemoteValues(t *testing.T) {
	t.Parallel()

	environment := productionEnvironment(t)
	environment["PROD_DNS_LABEL"] = "vermouth-test\nunsafe"
	environment["PROD_HOSTNAME"] = "vermouth-test\nunsafe.southeastasia.cloudapp.azure.com"
	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "prod-common.sh")},
		"prod_init\n",
		"",
		environment,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "PROD_DNS_LABEL must be a valid Azure DNS label")
}

// covers: AC-5
func TestRestrictedSSHEntrypointRejectsAnotherAction(t *testing.T) {
	t.Parallel()

	result := runCommand(
		t,
		"",
		map[string]string{
			"SSH_ORIGINAL_COMMAND": "shell " + strings.Repeat("a", 40) + " " + strings.Repeat("b", 64),
		},
		[]string{repoFile(t, "deploy", "production", "vermouth-ssh-entrypoint")},
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "deployment action is not allowed")
}

// covers: AC-5, AC-13
func TestRestrictedSSHEntrypointAllowsReadOnlyBaseInspectionWithNoPayload(t *testing.T) {
	t.Parallel()

	tools := t.TempDir()
	writeExecutable(t, tools, "sudo", "cat\n")
	environment := map[string]string{
		"PATH":                 tools + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SSH_ORIGINAL_COMMAND": "inspect-base " + strings.Repeat("a", 40) + " 123 2",
	}
	result := runCommand(
		t,
		"",
		environment,
		[]string{repoFile(t, "deploy", "production", "vermouth-ssh-entrypoint")},
	)

	require.NoError(t, result.err, result.stderr)
	require.JSONEq(t, `{
		"schema_version": 1,
		"action": "inspect-base",
		"git_sha": "`+strings.Repeat("a", 40)+`",
		"github_run_id": "123",
		"github_run_attempt": "2"
	}`, result.stdout)
}

// covers: AC-5, AC-13
func TestRestrictedSSHEntrypointRejectsABaseInspectionPayload(t *testing.T) {
	t.Parallel()

	result := runCommand(
		t,
		"unexpected",
		map[string]string{
			"SSH_ORIGINAL_COMMAND": "inspect-base " + strings.Repeat("a", 40) + " 123 2",
		},
		[]string{repoFile(t, "deploy", "production", "vermouth-ssh-entrypoint")},
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "inspect-base requires empty standard input")
}

// covers: AC-1, AC-8, AC-11
func TestProductionDoctorAcceptsTheExactAzureBoundary(t *testing.T) {
	t.Parallel()

	fixture := newAzureSnapshot(t, azureExactInboundRules())
	environment := productionEnvironment(t)
	maps.Copy(environment, fixture.environment)
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "production", "prod-common.sh"),
			repoFile(t, "deploy", "production", "prod-doctor.sh"),
		},
		"prod_init\nprod_validate_azure_snapshot \"$AZURE_ACCOUNT_ID\" \"$AZURE_VM_RESOURCE_ID\" \"$AZURE_VM_DOCUMENT\" \"$AZURE_NIC_DOCUMENT\" \"$AZURE_PUBLIC_IP_DOCUMENT\" \"$AZURE_RULES_DOCUMENT\"\n",
		"",
		environment,
	)

	require.NoError(t, result.err, result.stderr)
}

// covers: AC-1, AC-11
func TestProductionDoctorRejectsABroadAzureInboundRule(t *testing.T) {
	t.Parallel()

	fixture := newAzureSnapshot(t, azureBroadInboundRules())
	environment := productionEnvironment(t)
	maps.Copy(environment, fixture.environment)
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "production", "prod-common.sh"),
			repoFile(t, "deploy", "production", "prod-doctor.sh"),
		},
		"prod_init\nprod_validate_azure_snapshot \"$AZURE_ACCOUNT_ID\" \"$AZURE_VM_RESOURCE_ID\" \"$AZURE_VM_DOCUMENT\" \"$AZURE_NIC_DOCUMENT\" \"$AZURE_PUBLIC_IP_DOCUMENT\" \"$AZURE_RULES_DOCUMENT\"\n",
		"",
		environment,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "must expose Internet traffic only on ports 22, 80, and 443")
}

// covers: AC-1, AC-2
func TestAzurePublicIPUsesInstanceMetadataWhenPresent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeExecutable(t, directory, "curl", "exit 1\n")
	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "azure-metadata.sh")},
		"azure_public_ip \"$INSTANCE_METADATA\"\n",
		"",
		map[string]string{
			"PATH":              directory + string(os.PathListSeparator) + os.Getenv("PATH"),
			"INSTANCE_METADATA": `{"network":{"interface":[{"ipv4":{"ipAddress":[{"publicIpAddress":"4.194.251.123"}]}}]}}`,
		},
	)

	require.NoError(t, result.err, result.stderr)
	require.Equal(t, "4.194.251.123\n", result.stdout)
}

// covers: AC-1, AC-2
func TestAzurePublicIPFallsBackToStandardSKUMetadata(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeExecutable(t, directory, "curl", `
case "$*" in
  *metadata/loadbalancer*)
    printf '%s\n' '{"loadbalancer":{"publicIpAddresses":[{"frontendIpAddress":"4.194.251.123","privateIpAddress":"10.0.0.4"}]}}'
    ;;
  *) exit 1 ;;
esac
`)
	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "azure-metadata.sh")},
		"azure_public_ip \"$INSTANCE_METADATA\"\n",
		"",
		map[string]string{
			"PATH":              directory + string(os.PathListSeparator) + os.Getenv("PATH"),
			"INSTANCE_METADATA": `{"network":{"interface":[{"ipv4":{"ipAddress":[{"privateIpAddress":"10.0.0.4","publicIpAddress":""}]}}]}}`,
		},
	)

	require.NoError(t, result.err, result.stderr)
	require.Equal(t, "4.194.251.123\n", result.stdout)
}

// covers: AC-1, AC-2, AC-8
func TestRemoteDoctorChecksTheTraefikServiceInsteadOfHostSockets(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(repoFile(t, "deploy", "production", "remote-doctor.sh"))
	require.NoError(t, err)
	require.Contains(t, string(script), "get service traefik -o json")
	require.NotContains(t, string(script), "ss -ltnH")
}

// covers: AC-8
func TestProductionBootstrapWaitsForOneTraefikBeforeRequestingACertificate(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(repoFile(t, "deploy", "production", "bootstrap-root.sh"))
	require.NoError(t, err)
	removeProbe := bytes.Index(script, []byte("delete ingress acme-staging-probe"))
	installTraefik := bytes.Index(script, []byte("install -o root -g root -m 0644 \"$work/traefik-config.yaml\""))
	waitForOne := bytes.Index(script, []byte("the prior Traefik Pod did not leave the rollout"))
	createProbe := bytes.Index(script, []byte("kind: Service\nmetadata:\n  name: acme-staging-probe"))
	require.NotEqual(t, -1, removeProbe)
	require.NotEqual(t, -1, installTraefik)
	require.NotEqual(t, -1, waitForOne)
	require.NotEqual(t, -1, createProbe)
	require.Less(t, removeProbe, installTraefik)
	require.Less(t, installTraefik, waitForOne)
	require.Less(t, waitForOne, createProbe)
}

// covers: AC-17, AC-18
func TestProductionRecoveryAcceptsOneProtectedNativeAgeIdentity(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	identity := filepath.Join(directory, "identity.txt")
	recipient := "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	require.NoError(t, os.WriteFile(identity, []byte("AGE-SECRET-KEY-1AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"), 0o600))
	writeExecutable(t, directory, "age-keygen", "printf '%s\\n' \"$PROD_AGE_RECIPIENT\"\n")
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "production", "prod-common.sh"),
			repoFile(t, "deploy", "production", "prod-operations.sh"),
		},
		"prod_validate_age_identity\n",
		"",
		map[string]string{
			"PATH":               directory + string(os.PathListSeparator) + os.Getenv("PATH"),
			"PROD_AGE_IDENTITY":  identity,
			"PROD_AGE_RECIPIENT": recipient,
		},
	)

	require.NoError(t, result.err, result.stderr)
}

// covers: AC-17, AC-18
func TestProductionRecoveryRejectsMultipleAgeIdentities(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	identity := filepath.Join(directory, "identity.txt")
	recipient := "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	contents := strings.Repeat("AGE-SECRET-KEY-1AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n", 2)
	require.NoError(t, os.WriteFile(identity, []byte(contents), 0o600))
	writeExecutable(t, directory, "age-keygen", "printf '%s\\n' \"$PROD_AGE_RECIPIENT\"\n")
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "production", "prod-common.sh"),
			repoFile(t, "deploy", "production", "prod-operations.sh"),
		},
		"prod_validate_age_identity\n",
		"",
		map[string]string{
			"PATH":               directory + string(os.PathListSeparator) + os.Getenv("PATH"),
			"PROD_AGE_IDENTITY":  identity,
			"PROD_AGE_RECIPIENT": recipient,
		},
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "exactly one native age identity")
}

// covers: AC-2, AC-17, AC-18
func TestProductionBootstrapRequiresRecoveryToolsOnlyForDisasterBootstrap(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "prod-bootstrap.sh")},
		`mode=normal
prod_need() { printf '%s:%s\n' "$mode" "$1"; }
prod_bootstrap_tools
mode=restore
prod_restore_bootstrap_tools
`,
		"",
		nil,
	)

	require.NoError(t, result.err, result.stderr)
	require.NotContains(t, result.stdout, "normal:age\n")
	require.NotContains(t, result.stdout, "normal:age-keygen\n")
	require.Contains(t, result.stdout, "restore:age\n")
	require.Contains(t, result.stdout, "restore:age-keygen\n")
	require.Contains(t, result.stdout, "restore:zstd\n")
}

// covers: AC-2, AC-5
func TestProductionBootstrapDisablesAppleArchiveMetadata(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(repoFile(t, "deploy", "production", "prod-bootstrap.sh"))
	require.NoError(t, err)
	require.Contains(t, string(script), "COPYFILE_DISABLE=1 tar")
}

// covers: AC-13, AC-15
func TestProductionJobRunIdentityIncludesTheWorkflowAttempt(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "deploy-root-lib.sh")},
		"production_job_run_id 123456 2\n",
		"",
		nil,
	)

	require.NoError(t, result.err, result.stderr)
	require.Equal(t, "123456-2\n", result.stdout)
}

// covers: AC-13, AC-20
func TestProductionInspectionUsesHelmFourListSemantics(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(repoFile(t, "deploy", "production", "deploy-root-lib.sh"))
	require.NoError(t, err)
	require.Contains(t, string(script), "list --filter '^vermouth$'")
	require.NotContains(t, string(script), "list --all")
}

// covers: AC-19
func TestProductionCapacitySamplesOnlyVermouthAndK3sPods(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	releaseWork := filepath.Join(directory, "release")
	require.NoError(t, os.Mkdir(releaseWork, 0o700))
	writeExecutable(t, directory, "kubectl", `
printf '%s\n' "$*" >>"$CALL_LOG"
case "$*" in
  "top pod --namespace vermouth --no-headers") printf '%s\n' 'gateway-abc 5m 10Mi' ;;
  "top pod --namespace kube-system --no-headers") printf '%s\n' 'traefik-abc 8m 20Mi' ;;
  *) exit 1 ;;
esac
`)
	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "deploy-root-lib.sh")},
		"release_work=$RELEASE_WORK\nproduction_pod_usage\n",
		"",
		map[string]string{
			"PATH":         directory + string(os.PathListSeparator) + os.Getenv("PATH"),
			"CALL_LOG":     callLog,
			"RELEASE_WORK": releaseWork,
		},
	)

	require.NoError(t, result.err, result.stderr)
	require.JSONEq(t, `[
  {"namespace":"kube-system","pod":"traefik-abc","cpu":"8m","memory":"20Mi"},
  {"namespace":"vermouth","pod":"gateway-abc","cpu":"5m","memory":"10Mi"}
]`, result.stdout)
	calls, err := os.ReadFile(callLog)
	require.NoError(t, err)
	require.Equal(t, "top pod --namespace vermouth --no-headers\ntop pod --namespace kube-system --no-headers\n", string(calls))
	require.NotContains(t, string(calls), "--all-namespaces")
}

// covers: AC-13, AC-19
func TestProductionExternalReadinessReturnsControlForRecovery(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	dateState := filepath.Join(directory, "date-state")
	writeExecutable(t, directory, "curl", "exit 1\n")
	writeExecutable(t, directory, "sleep", ":\n")
	writeExecutable(t, directory, "date", `
if [ ! -f "$DATE_STATE" ]; then
  printf '%s\n' 1 >"$DATE_STATE"
  printf '%s\n' 100
else
  printf '%s\n' 400
fi
`)
	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "deploy-root-lib.sh")},
		"if wait_for_external_release example.invalid; then exit 1; fi\nprintf '%s\\n' recovery\n",
		"",
		map[string]string{
			"PATH":       directory + string(os.PathListSeparator) + os.Getenv("PATH"),
			"DATE_STATE": dateState,
		},
	)

	require.NoError(t, result.err, result.stderr)
	require.Equal(t, "recovery\n", result.stdout)
}

// covers: AC-14
func TestProductionRollbackDefersConfirmationToTheLockedRemoteOperation(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{repoFile(t, "deploy", "production", "prod-operations.sh")},
		`prod_operator_init() { :; }
prod_ssh() { printf '%s\n' "$1"; }
prod_rollback
`,
		"",
		nil,
	)

	require.NoError(t, result.err, result.stderr)
	require.Equal(t, "sudo --non-interactive /usr/local/sbin/vermouth-prod-ops rollback\n", result.stdout)
}

// covers: AC-15, AC-16
func TestProductionLogsRejectMultilineRemoteArguments(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "production", "prod-common.sh"),
			repoFile(t, "deploy", "production", "prod-operations.sh"),
		},
		`prod_operator_init() { :; }
prod_ssh() { printf '%s\n' "$1"; }
prod_logs migrate-identity-123-2 "--since=10m
unsafe"
`,
		"",
		nil,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "unknown production log option")
}

type azureSnapshot struct {
	environment map[string]string
}

func newAzureSnapshot(t *testing.T, rules string) azureSnapshot {
	t.Helper()

	const (
		subscriptionID = "11111111-1111-1111-1111-111111111111"
		vmResourceID   = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/vermouth/providers/Microsoft.Compute/virtualMachines/nguyenducloc-vm2"
		nicResourceID  = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/vermouth/providers/Microsoft.Network/networkInterfaces/vermouth-nic"
		ipResourceID   = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/vermouth/providers/Microsoft.Network/publicIPAddresses/vermouth-ip"
	)
	directory := t.TempDir()
	vm := `{"id":"` + vmResourceID + `","name":"nguyenducloc-vm2","location":"southeastasia","hardwareProfile":{"vmSize":"Standard_D2ads_v6"},"networkProfile":{"networkInterfaces":[{"id":"` + nicResourceID + `"}]}}`
	nic := `{"id":"` + nicResourceID + `","ipConfigurations":[{"publicIPAddress":{"id":"` + ipResourceID + `"}}]}`
	publicIP := `{"id":"` + ipResourceID + `","publicIPAllocationMethod":"Static","ipAddress":"4.194.251.123","dnsSettings":{"domainNameLabel":"vermouth-test","fqdn":"vermouth-test.southeastasia.cloudapp.azure.com"}}`
	writeProductionFixture(t, directory, "vm.json", vm)
	writeProductionFixture(t, directory, "nic.json", nic)
	writeProductionFixture(t, directory, "public-ip.json", publicIP)
	writeProductionFixture(t, directory, "rules.json", rules)
	return azureSnapshot{environment: map[string]string{
		"AZURE_ACCOUNT_ID":         subscriptionID,
		"AZURE_VM_RESOURCE_ID":     vmResourceID,
		"AZURE_VM_DOCUMENT":        filepath.Join(directory, "vm.json"),
		"AZURE_NIC_DOCUMENT":       filepath.Join(directory, "nic.json"),
		"AZURE_PUBLIC_IP_DOCUMENT": filepath.Join(directory, "public-ip.json"),
		"AZURE_RULES_DOCUMENT":     filepath.Join(directory, "rules.json"),
	}}
}

func azureExactInboundRules() string {
	return `[{"effectiveSecurityRules":[{"direction":"Inbound","access":"Allow","sourceAddressPrefix":"Internet","destinationPortRange":"22"},{"direction":"Inbound","access":"Allow","sourceAddressPrefix":"Internet","destinationPortRange":"80"},{"direction":"Inbound","access":"Allow","sourceAddressPrefix":"Internet","destinationPortRange":"443"}]}]`
}

func azureBroadInboundRules() string {
	return `[{"effectiveSecurityRules":[{"direction":"Inbound","access":"Allow","sourceAddressPrefix":"Internet","destinationPortRange":"*"}]}]`
}

func writeProductionFixture(t *testing.T, directory, name, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600))
}

func productionEnvironment(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"PROD_SCRIPT_DIR":        repoFile(t, "deploy", "production"),
		"PROD_HOST":              "4.194.251.123",
		"PROD_DNS_LABEL":         "vermouth-test",
		"PROD_HOSTNAME":          "vermouth-test.southeastasia.cloudapp.azure.com",
		"PROD_SSH_USER":          "vermouth-deploy",
		"PROD_K3S_LIVE_VERSION":  "v1.36.3+k3s1",
		"PROD_K3S_IMAGE_TAG":     "v1.36.3-k3s1",
		"PROD_TRAEFIK_IMAGE":     "rancher/mirrored-library-traefik:3.7.8",
		"PROD_TRAEFIK_CHART":     "40.1.4+up40.1.0",
		"PROD_STORAGE_UUID":      "37f07a74-8426-45fd-8e55-79515376a136",
		"PROD_STORAGE_ROOT":      "/var/lib/vermouth",
		"PROD_AGE_RECIPIENT":     "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",
		"DOCKERHUB_NAMESPACE":    "vermouthtest",
		"PROD_ACME_EMAIL":        "operator@example.com",
		"PROD_DEPLOY_PUBLIC_KEY": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA test-key",
		"PROD_DEPLOY_KEY_ID":     "test-key",
	}
}
