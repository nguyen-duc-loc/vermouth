//nolint:err113,gocognit,gocritic,gosec,noinlineerr,revive,tagliatelle // The strict structs mirror committed external field names and report the exact bad path.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

const (
	schemaVersion        = 1
	computeResourceCount = 2
)

const (
	platformARM64            = "linux/arm64"
	platformAMD64            = "linux/amd64"
	registryName             = "vermouth-registry"
	workloadGoApplication    = "go-application"
	workloadWeb              = "web"
	workloadPostgres         = "postgres"
	workloadRedpanda         = "redpanda"
	workloadGarage           = "garage"
	workloadMigration        = "migration"
	workloadGarageInit       = "garage-init"
	workloadDevelopmentToken = "devtoken"
	environmentGoogleEnabled = "google-enabled"
	kindJob                  = "Job"
)

type versionRange struct {
	Series  string `yaml:"series"`
	Minimum string `yaml:"minimum"`
}

type versionsConfig struct {
	SchemaVersion int `yaml:"schemaVersion"`
	Host          struct {
		OS                     string `yaml:"os"`
		Architecture           string `yaml:"architecture"`
		ColimaProfile          string `yaml:"colimaProfile"`
		DockerContext          string `yaml:"dockerContext"`
		DockerEndpointTemplate string `yaml:"dockerEndpointTemplate"`
		MinimumCPUs            int    `yaml:"minimumCPUs"`
		MinimumMemoryGiB       int    `yaml:"minimumMemoryGiB"`
	} `yaml:"host"`
	Builder struct {
		Name            string   `yaml:"name"`
		Driver          string   `yaml:"driver"`
		Status          string   `yaml:"status"`
		BuildkitVersion string   `yaml:"buildkitVersion"`
		Platforms       []string `yaml:"platforms"`
	} `yaml:"builder"`
	Tools struct {
		Colima       versionRange `yaml:"colima"`
		DockerClient versionRange `yaml:"dockerClient"`
		DockerServer versionRange `yaml:"dockerServer"`
		Buildx       versionRange `yaml:"buildx"`
		K3D          versionRange `yaml:"k3d"`
		Kubectl      struct {
			AcceptedSeries []string `yaml:"acceptedSeries"`
		} `yaml:"kubectl"`
		Helm versionRange `yaml:"helm"`
		Task versionRange `yaml:"task"`
		Go   versionRange `yaml:"go"`
		Node versionRange `yaml:"node"`
		PNPM struct {
			Exact string `yaml:"exact"`
		} `yaml:"pnpm"`
	} `yaml:"tools"`
	Cluster struct {
		Name           string `yaml:"name"`
		Context        string `yaml:"context"`
		Servers        int    `yaml:"servers"`
		Agents         int    `yaml:"agents"`
		K3SImageTag    string `yaml:"k3sImageTag"`
		K3SLiveVersion string `yaml:"k3sLiveVersion"`
		NodeName       string `yaml:"nodeName"`
		NetworkName    string `yaml:"networkName"`
	} `yaml:"cluster"`
	Bindings struct {
		ApplicationHost string `yaml:"applicationHost"`
		ApplicationPort int    `yaml:"applicationPort"`
		RegistryHost    string `yaml:"registryHost"`
		RegistryPort    int    `yaml:"registryPort"`
	} `yaml:"bindings"`
	Registry struct {
		K3DName         string `yaml:"k3dName"`
		InventoryName   string `yaml:"inventoryName"`
		ContainerName   string `yaml:"containerName"`
		NetworkAlias    string `yaml:"networkAlias"`
		HostEndpoint    string `yaml:"hostEndpoint"`
		ClusterEndpoint string `yaml:"clusterEndpoint"`
		MirrorKey       string `yaml:"mirrorKey"`
		MirrorEndpoint  string `yaml:"mirrorEndpoint"`
		DataVolume      string `yaml:"dataVolume"`
	} `yaml:"registry"`
	Traefik struct {
		Release      string `yaml:"release"`
		Namespace    string `yaml:"namespace"`
		Image        string `yaml:"image"`
		ImageVersion string `yaml:"imageVersion"`
		ImageDigest  string `yaml:"imageDigest"`
		ChartVersion string `yaml:"chartVersion"`
	} `yaml:"traefik"`
	Storage struct {
		DataVolume string `yaml:"dataVolume"`
		RootPath   string `yaml:"rootPath"`
		ClassName  string `yaml:"className"`
		Labels     struct {
			PlatformKey   string `yaml:"platformKey"`
			PlatformValue string `yaml:"platformValue"`
			RoleKey       string `yaml:"roleKey"`
		} `yaml:"labels"`
		Roles struct {
			Data     string `yaml:"data"`
			Registry string `yaml:"registry"`
		} `yaml:"roles"`
	} `yaml:"storage"`
	Deadlines struct {
		DoctorSeconds     int `yaml:"doctorSeconds"`
		BootstrapSeconds  int `yaml:"bootstrapSeconds"`
		ColdDevSeconds    int `yaml:"coldDevSeconds"`
		WarmDevSeconds    int `yaml:"warmDevSeconds"`
		StatusSeconds     int `yaml:"statusSeconds"`
		LogsSeconds       int `yaml:"logsSeconds"`
		StopSeconds       int `yaml:"stopSeconds"`
		CleanSeconds      int `yaml:"cleanSeconds"`
		RecreateSeconds   int `yaml:"recreateSeconds"`
		JobsCleanSeconds  int `yaml:"jobsCleanSeconds"`
		CacheCleanSeconds int `yaml:"cacheCleanSeconds"`
		MeasureSeconds    int `yaml:"measureSeconds"`
		LockRenewSeconds  int `yaml:"lockRenewSeconds"`
		StaleGraceSeconds int `yaml:"staleGraceSeconds"`
	} `yaml:"deadlines"`
	DeletionInventory struct {
		Clusters           []string `yaml:"clusters"`
		Registries         []string `yaml:"registries"`
		Volumes            []string `yaml:"volumes"`
		GeneratedStateRoot string   `yaml:"generatedStateRoot"`
	} `yaml:"deletionInventory"`
	ImmutableIdentityFields []string `yaml:"immutableIdentityFields"`
}

type externalImage struct {
	Image     string   `yaml:"image"`
	Digest    string   `yaml:"digest"`
	Platforms []string `yaml:"platforms"`
}

type builtImage struct {
	Workload          string            `yaml:"workload"`
	RepositoryPath    string            `yaml:"repositoryPath"`
	Dockerfile        string            `yaml:"dockerfile"`
	Target            string            `yaml:"target"`
	NativePlatforms   []string          `yaml:"nativePlatforms"`
	MultiPlatforms    []string          `yaml:"multiPlatforms"`
	Inputs            []string          `yaml:"inputs"`
	BuildArgs         map[string]string `yaml:"buildArgs"`
	BaseLocks         []string          `yaml:"baseLocks"`
	HashSchema        string            `yaml:"hashSchema"`
	NativeTagTemplate string            `yaml:"nativeTagTemplate"`
	MultiTagTemplate  string            `yaml:"multiTagTemplate"`
	ChildTagTemplate  string            `yaml:"childTagTemplate"`
}

type imageLock struct {
	SchemaVersion int                      `yaml:"schemaVersion"`
	External      map[string]externalImage `yaml:"external"`
	Probe         struct {
		Dockerfile     string   `yaml:"dockerfile"`
		InputSHA256    string   `yaml:"inputSHA256"`
		Platform       string   `yaml:"platform"`
		RepositoryPath string   `yaml:"repositoryPath"`
		BaseLocks      []string `yaml:"baseLocks"`
	} `yaml:"probe"`
	Built map[string]builtImage `yaml:"built"`
}

type inventoryKey struct {
	Source     string `yaml:"source"`
	Generation string `yaml:"generation"`
	Optional   bool   `yaml:"optional,omitempty"`
}

type databaseInventory struct {
	ServiceDNS     string `yaml:"serviceDNS"`
	Port           int    `yaml:"port"`
	Database       string `yaml:"database"`
	Role           string `yaml:"role"`
	PasswordSecret string `yaml:"passwordSecret"`
	PasswordKey    string `yaml:"passwordKey"`
	URLTemplate    string `yaml:"urlTemplate"`
}

type secretEntry struct {
	NameBase  string                  `yaml:"nameBase"`
	Database  *databaseInventory      `yaml:"database,omitempty"`
	Keys      map[string]inventoryKey `yaml:"keys"`
	Consumers []string                `yaml:"consumers"`
}

type secretInventory struct {
	SchemaVersion int                    `yaml:"schemaVersion"`
	HashSchema    string                 `yaml:"hashSchema"`
	Foundation    map[string]secretEntry `yaml:"foundation"`
	Runtime       map[string]secretEntry `yaml:"runtime"`
}

type matrixPort struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Port     int    `yaml:"port"`
}

type selectorExpression struct {
	Key    string   `yaml:"key"`
	Values []string `yaml:"values"`
}

type ipBlock struct {
	CIDR   string   `yaml:"cidr"`
	Except []string `yaml:"except,omitempty"`
}

type trafficPeer struct {
	NamespaceSelector     map[string]string   `yaml:"namespaceSelector,omitempty"`
	PodSelector           map[string]string   `yaml:"podSelector,omitempty"`
	PodSelectorExpression *selectorExpression `yaml:"podSelectorExpression,omitempty"`
	IPBlock               *ipBlock            `yaml:"ipBlock,omitempty"`
}

type trafficRow struct {
	ID                    string              `yaml:"id"`
	PolicyName            string              `yaml:"policyName"`
	Direction             string              `yaml:"direction"`
	Environment           string              `yaml:"environment"`
	Order                 int                 `yaml:"order"`
	PodSelector           map[string]string   `yaml:"podSelector,omitempty"`
	PodSelectorExpression *selectorExpression `yaml:"podSelectorExpression,omitempty"`
	Peers                 []trafficPeer       `yaml:"peers"`
	Ports                 []matrixPort        `yaml:"ports"`
}

type trafficMatrix struct {
	SchemaVersion int          `yaml:"schemaVersion"`
	Rows          []trafficRow `yaml:"rows"`
}

type workloadProbe struct {
	Type             string   `yaml:"type"`
	Path             string   `yaml:"path,omitempty"`
	Command          []string `yaml:"command,omitempty"`
	PeriodSeconds    int      `yaml:"periodSeconds"`
	TimeoutSeconds   int      `yaml:"timeoutSeconds"`
	FailureThreshold int      `yaml:"failureThreshold"`
}

type writableMount struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	Medium    string `yaml:"medium"`
	SizeLimit string `yaml:"sizeLimit"`
}

type workloadEntry struct {
	Kind                         string `yaml:"kind"`
	Replicas                     int    `yaml:"replicas"`
	ServiceAccount               string `yaml:"serviceAccount"`
	AutomountServiceAccountToken bool   `yaml:"automountServiceAccountToken"`
	RunAsUser                    int    `yaml:"runAsUser"`
	RunAsGroup                   int    `yaml:"runAsGroup"`
	FSGroup                      int    `yaml:"fsGroup,omitempty"`
	ReadOnlyRootFilesystem       bool   `yaml:"readOnlyRootFilesystem"`
	ActiveDeadlineSeconds        int    `yaml:"activeDeadlineSeconds,omitempty"`
	BackoffLimit                 int    `yaml:"backoffLimit,omitempty"`
	Resources                    struct {
		Requests map[string]string `yaml:"requests"`
		Limits   map[string]string `yaml:"limits"`
	} `yaml:"resources"`
	Readiness      *workloadProbe  `yaml:"readiness,omitempty"`
	Liveness       *workloadProbe  `yaml:"liveness,omitempty"`
	WritableMounts []writableMount `yaml:"writableMounts"`
}

type workloadMatrix struct {
	SchemaVersion int                      `yaml:"schemaVersion"`
	Workloads     map[string]workloadEntry `yaml:"workloads"`
}

func strictYAML[T any](path string) (T, error) {
	var value T
	content, err := os.ReadFile(path)
	if err != nil {
		return value, fmt.Errorf("open %s: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode %s: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, fmt.Errorf("decode %s: expected one YAML document", path)
	}
	return value, nil
}

func validateInputs(root string) error {
	versionsPath := filepath.Join(root, "deploy/platform/versions.yaml")
	versions, err := strictYAML[versionsConfig](versionsPath)
	if err != nil {
		return err
	}
	if err := validateVersions(versions); err != nil {
		return fmt.Errorf("%s: %w", versionsPath, err)
	}

	lockPath := filepath.Join(root, "deploy/images.lock.yaml")
	lock, err := strictYAML[imageLock](lockPath)
	if err != nil {
		return err
	}
	if err := validateImageLock(root, lock); err != nil {
		return fmt.Errorf("%s: %w", lockPath, err)
	}

	inventoryPath := filepath.Join(root, "deploy/platform/secret-inventory.yaml")
	inventory, err := strictYAML[secretInventory](inventoryPath)
	if err != nil {
		return err
	}
	if err := validateSecretInventory(inventory); err != nil {
		return fmt.Errorf("%s: %w", inventoryPath, err)
	}

	trafficPath := filepath.Join(root, "deploy/helm/vermouth/files/traffic-matrix.yaml")
	traffic, err := strictYAML[trafficMatrix](trafficPath)
	if err != nil {
		return err
	}
	if err := validateTrafficMatrix(traffic); err != nil {
		return fmt.Errorf("%s: %w", trafficPath, err)
	}

	workloadPath := filepath.Join(root, "deploy/helm/vermouth/files/workload-matrix.yaml")
	workloads, err := strictYAML[workloadMatrix](workloadPath)
	if err != nil {
		return err
	}
	if err := validateWorkloadMatrix(workloads); err != nil {
		return fmt.Errorf("%s: %w", workloadPath, err)
	}

	for _, schemaPath := range []string{
		"deploy/platform/images.schema.json",
		"deploy/platform/secrets.schema.json",
		"deploy/helm/vermouth/values.schema.json",
	} {
		compiler := jsonschema.NewCompiler()
		if _, err := compiler.Compile(filepath.Join(root, schemaPath)); err != nil {
			return fmt.Errorf("compile %s: %w", schemaPath, err)
		}
	}

	return validateK3DConfig(root, versions)
}

func validateVersions(config versionsConfig) error {
	if config.SchemaVersion != schemaVersion {
		return fmt.Errorf("schemaVersion is %d, expected 1", config.SchemaVersion)
	}
	if config.Host.ColimaProfile != "default" || config.Host.DockerContext != "colima" {
		return errors.New("host must use Colima profile default and Docker context colima")
	}
	if config.Builder.Name != "colima" || config.Builder.Driver != "docker" {
		return errors.New("builder must be colima with the docker driver")
	}
	if !slices.Equal(config.Builder.Platforms, []string{platformARM64, platformAMD64}) {
		return fmt.Errorf("builder platforms are %v, expected linux/arm64 then linux/amd64", config.Builder.Platforms)
	}
	if config.Registry.K3DName != registryName ||
		config.Registry.InventoryName != registryName ||
		config.Registry.ContainerName != registryName ||
		config.Registry.NetworkAlias != registryName {
		return errors.New("every registry name must be exactly vermouth-registry")
	}
	if config.Registry.HostEndpoint != "127.0.0.1:5111" {
		return fmt.Errorf("registry host endpoint is %q", config.Registry.HostEndpoint)
	}
	if config.Registry.ClusterEndpoint != "vermouth-registry:5000" ||
		config.Registry.MirrorKey != "vermouth-registry:5000" ||
		config.Registry.MirrorEndpoint != "http://vermouth-registry:5000" {
		return errors.New("registry cluster endpoint and mirror must use vermouth-registry:5000")
	}
	if net.ParseIP(config.Bindings.ApplicationHost) == nil ||
		config.Bindings.ApplicationHost != "127.0.0.1" ||
		config.Bindings.RegistryHost != "127.0.0.1" {
		return errors.New("application and registry binds must use IPv4 loopback")
	}
	if config.Bindings.ApplicationPort != 8080 || config.Bindings.RegistryPort != 5111 {
		return fmt.Errorf(
			"host ports are application %d and registry %d, expected 8080 and 5111",
			config.Bindings.ApplicationPort,
			config.Bindings.RegistryPort,
		)
	}
	if len(config.ImmutableIdentityFields) == 0 {
		return errors.New("immutableIdentityFields must not be empty")
	}
	if config.Deadlines.CacheCleanSeconds < 1 {
		return errors.New("cacheCleanSeconds must be positive")
	}
	return nil
}

func validateImageLock(root string, lock imageLock) error {
	if lock.SchemaVersion != schemaVersion {
		return fmt.Errorf("schemaVersion is %d, expected 1", lock.SchemaVersion)
	}
	if len(lock.External) == 0 || len(lock.Built) == 0 {
		return errors.New("external and built image inventories must not be empty")
	}
	if err := validatePlatformProbe(root, lock); err != nil {
		return err
	}

	for name, external := range lock.External {
		if !strings.HasPrefix(external.Digest, "sha256:") || len(external.Digest) != 71 {
			return fmt.Errorf("external image %q has invalid digest", name)
		}
		if !slices.Equal(external.Platforms, []string{platformARM64, platformAMD64}) {
			return fmt.Errorf("external image %q platforms must be arm64 then amd64", name)
		}
	}

	for key, image := range lock.Built {
		if key != image.Workload {
			return fmt.Errorf("built key %q does not match workload %q", key, image.Workload)
		}
		if image.RepositoryPath != "vermouth/"+image.Workload {
			return fmt.Errorf("built image %q has repositoryPath %q", key, image.RepositoryPath)
		}
		if image.HashSchema != "vermouth-image-v1" {
			return fmt.Errorf("built image %q has hashSchema %q", key, image.HashSchema)
		}
		if !slices.Equal(image.NativePlatforms, []string{platformARM64}) {
			return fmt.Errorf("built image %q nativePlatforms must contain only linux/arm64", key)
		}
		wantMulti := []string{platformARM64, platformAMD64}
		if key == workloadDevelopmentToken {
			wantMulti = []string{}
		}
		if !slices.Equal(image.MultiPlatforms, wantMulti) {
			return fmt.Errorf("built image %q has invalid multiPlatforms", key)
		}
		if image.NativeTagTemplate != "dev-{hash12}" {
			return fmt.Errorf("built image %q has invalid nativeTagTemplate", key)
		}
		if key != workloadDevelopmentToken &&
			(image.MultiTagTemplate != "multi-{hash12}" ||
				image.ChildTagTemplate != "multi-{hash12}-{architecture}") {
			return fmt.Errorf("built image %q has invalid multiple architecture tag templates", key)
		}
		if _, err := os.Stat(filepath.Join(root, image.Dockerfile)); err != nil {
			return fmt.Errorf("built image %q dockerfile: %w", key, err)
		}
		if err := validateDeclaredInputs(root, image.Inputs); err != nil {
			return fmt.Errorf("built image %q inputs: %w", key, err)
		}
		for _, base := range image.BaseLocks {
			if _, ok := lock.External[base]; !ok {
				return fmt.Errorf("built image %q names unknown base lock %q", key, base)
			}
		}
	}
	return nil
}

func validatePlatformProbe(root string, lock imageLock) error {
	probePath := filepath.Join(root, lock.Probe.Dockerfile)
	probeContent, err := os.ReadFile(probePath)
	if err != nil {
		return fmt.Errorf("probe Dockerfile: %w", err)
	}
	probeHash := sha256.Sum256(probeContent)
	if fmt.Sprintf("%x", probeHash) != lock.Probe.InputSHA256 ||
		lock.Probe.Platform != platformARM64 ||
		lock.Probe.RepositoryPath != "vermouth/platform-probe" {
		return errors.New("platform probe identity differs from its committed lock")
	}
	for _, base := range lock.Probe.BaseLocks {
		if _, ok := lock.External[base]; !ok {
			return fmt.Errorf("platform probe names unknown base lock %q", base)
		}
	}
	return nil
}

func validateDeclaredInputs(root string, inputs []string) error {
	if len(inputs) == 0 {
		return errors.New("at least one input is required")
	}
	cleaned := make([]string, 0, len(inputs))
	for _, input := range inputs {
		clean := filepath.ToSlash(filepath.Clean(input))
		if clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(input) {
			return fmt.Errorf("input %q is outside the repository", input)
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(clean))); err != nil {
			return fmt.Errorf("input %q: %w", input, err)
		}
		cleaned = append(cleaned, clean)
	}
	slices.Sort(cleaned)
	for index, input := range cleaned {
		if index > 0 {
			previous := cleaned[index-1]
			overlaps := input == previous || strings.HasPrefix(input+"/", previous+"/")
			if overlaps {
				return fmt.Errorf("input %q overlaps %q", input, previous)
			}
		}
	}
	return nil
}

func validateSecretInventory(inventory secretInventory) error {
	if inventory.SchemaVersion != schemaVersion || inventory.HashSchema != "vermouth-secret-v1" {
		return errors.New("expected schemaVersion 1 and hashSchema vermouth-secret-v1")
	}
	if len(inventory.Foundation) == 0 || len(inventory.Runtime) == 0 {
		return errors.New("foundation and runtime inventories must not be empty")
	}

	for group, entries := range map[string]map[string]secretEntry{
		"foundation": inventory.Foundation,
		"runtime":    inventory.Runtime,
	} {
		for key, entry := range entries {
			if entry.NameBase == "" || len(entry.Keys) == 0 || len(entry.Consumers) == 0 {
				return fmt.Errorf("%s row %q is incomplete", group, key)
			}
			if entry.Database != nil {
				want := "postgres://{role}:{password}@{serviceDNS}:{port}/{database}?sslmode=disable"
				if entry.Database.URLTemplate != want || entry.Database.Port != 5432 {
					return fmt.Errorf("%s row %q has invalid database contract", group, key)
				}
			}
		}
	}
	return nil
}

func validateTrafficMatrix(matrix trafficMatrix) error {
	if matrix.SchemaVersion != schemaVersion || len(matrix.Rows) == 0 {
		return errors.New("expected schemaVersion 1 and at least one traffic row")
	}
	ids := map[string]struct{}{}
	orders := map[int]struct{}{}
	policies := map[string]string{}
	allowedSelectorKeys := map[string]struct{}{
		"app.kubernetes.io/name":      {},
		"app.kubernetes.io/component": {},
		"kubernetes.io/metadata.name": {},
		"k8s-app":                     {},
	}
	for _, row := range matrix.Rows {
		if _, exists := ids[row.ID]; exists {
			return fmt.Errorf("duplicate row id %q", row.ID)
		}
		ids[row.ID] = struct{}{}
		if _, exists := orders[row.Order]; exists {
			return fmt.Errorf("duplicate row order %d", row.Order)
		}
		orders[row.Order] = struct{}{}
		if row.Direction != "Ingress" && row.Direction != "Egress" && row.Direction != "Both" {
			return fmt.Errorf("row %q has invalid direction %q", row.ID, row.Direction)
		}
		if row.Environment != "all" && row.Environment != environmentGoogleEnabled {
			return fmt.Errorf("row %q has invalid environment %q", row.ID, row.Environment)
		}
		signature := fmt.Sprintf("%s|%v|%v", row.Direction, row.PodSelector, row.PodSelectorExpression)
		if previous, exists := policies[row.PolicyName]; exists && previous != signature {
			return fmt.Errorf("policy %q has inconsistent target or direction", row.PolicyName)
		}
		policies[row.PolicyName] = signature
		if err := validateSelectorKeys(row.PodSelector, allowedSelectorKeys); err != nil {
			return fmt.Errorf("row %q: %w", row.ID, err)
		}
		for _, peer := range row.Peers {
			if err := validateSelectorKeys(peer.NamespaceSelector, allowedSelectorKeys); err != nil {
				return fmt.Errorf("row %q: %w", row.ID, err)
			}
			if err := validateSelectorKeys(peer.PodSelector, allowedSelectorKeys); err != nil {
				return fmt.Errorf("row %q: %w", row.ID, err)
			}
		}
		for _, port := range row.Ports {
			if port.Name == "" || (port.Protocol != "TCP" && port.Protocol != "UDP") || port.Port < 1 {
				return fmt.Errorf("row %q has invalid named port", row.ID)
			}
		}
	}
	return nil
}

func validateWorkloadMatrix(matrix workloadMatrix) error {
	required := []string{
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
	if matrix.SchemaVersion != schemaVersion {
		return fmt.Errorf("schemaVersion is %d, expected 1", matrix.SchemaVersion)
	}
	for _, name := range required {
		workload, ok := matrix.Workloads[name]
		if !ok {
			return fmt.Errorf("workload %q is missing", name)
		}
		if workload.Replicas != 1 || workload.ServiceAccount == "" {
			return fmt.Errorf("workload %q has invalid replicas or ServiceAccount", name)
		}
		if !hasCPUAndMemory(workload.Resources.Requests) || !hasCPUAndMemory(workload.Resources.Limits) {
			return fmt.Errorf("workload %q must set CPU and memory requests and limits", name)
		}
	}
	return nil
}

func hasCPUAndMemory(resources map[string]string) bool {
	if len(resources) != computeResourceCount {
		return false
	}
	_, hasCPU := resources["cpu"]
	_, hasMemory := resources["memory"]
	return hasCPU && hasMemory
}

func validateK3DConfig(root string, versions versionsConfig) error {
	path := filepath.Join(root, "deploy/k3d/vermouth.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	required := []string{
		"name: " + versions.Cluster.Name,
		"servers: 1",
		"agents: 0",
		versions.Cluster.K3SImageTag,
		fmt.Sprintf(
			"port: %s:%d:80",
			versions.Bindings.ApplicationHost,
			versions.Bindings.ApplicationPort,
		),
		"volume: " + versions.Storage.DataVolume + ":" + versions.Storage.RootPath,
		"name: " + versions.Registry.K3DName,
		"host: 127.0.0.1",
		"hostPort: \"5111\"",
		versions.Registry.DataVolume + ":/var/lib/registry",
	}
	for _, value := range required {
		if !bytes.Contains(content, []byte(value)) {
			return fmt.Errorf("%s does not contain %q", path, value)
		}
	}
	return nil
}

func validateSelectorKeys(selector map[string]string, allowed map[string]struct{}) error {
	for key := range selector {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown selector key %q", key)
		}
	}
	return nil
}

func getValue(path, wanted string, output io.Writer) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	value, err := decodeSingleYAML(content, path)
	if err != nil {
		return err
	}
	current := value
	for part := range strings.SplitSeq(wanted, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return fmt.Errorf("path %q does not select an object at %q", wanted, part)
		}
		current, ok = object[part]
		if !ok {
			return fmt.Errorf("path %q is absent", wanted)
		}
	}
	switch scalar := current.(type) {
	case string:
		_, err = fmt.Fprintln(output, scalar)
	case int:
		_, err = fmt.Fprintln(output, scalar)
	case bool:
		_, err = fmt.Fprintln(output, scalar)
	default:
		var encoded []byte
		encoded, err = json.Marshal(current)
		if err == nil {
			_, err = fmt.Fprintln(output, string(encoded))
		}
	}
	if err != nil {
		return fmt.Errorf("write value: %w", err)
	}
	return nil
}

func decodeSingleYAML(content []byte, path string) (any, error) {
	var value any
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s: expected one YAML document", path)
	}
	return value, nil
}

func validateDocument(schemaPath, documentPath string) error {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		return fmt.Errorf("compile %s: %w", schemaPath, err)
	}
	content, err := os.ReadFile(documentPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", documentPath, err)
	}

	var document any
	formatPath := strings.TrimSuffix(documentPath, ".next")
	extension := strings.ToLower(filepath.Ext(formatPath))
	if extension == ".yaml" || extension == ".yml" {
		document, err = decodeSingleYAML(content, documentPath)
		if err != nil {
			return err
		}
	} else {
		document, err = jsonschema.UnmarshalJSON(bytes.NewReader(content))
		if err != nil {
			return fmt.Errorf("decode %s: %w", documentPath, err)
		}
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("validate %s: %w", documentPath, err)
	}
	return nil
}
