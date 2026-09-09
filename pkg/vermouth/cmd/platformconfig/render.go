//nolint:err113,gocritic,gosec,noinlineerr // Validation errors name the exact rendered component and mismatched value.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type manifest = map[string]any

const (
	applicationWorkloadCount = 5
	serviceCount             = 4
)

type renderedPaths struct {
	traffic       string
	workloads     string
	foundation    string
	applications  string
	jobs          string
	googleEnabled bool
}

func validateRendered(paths renderedPaths) error {
	traffic, err := strictYAML[trafficMatrix](paths.traffic)
	if err != nil {
		return err
	}
	workloads, err := strictYAML[workloadMatrix](paths.workloads)
	if err != nil {
		return err
	}
	documents := []manifest{}
	for _, path := range []string{paths.foundation, paths.applications, paths.jobs} {
		parsed, err := readManifests(path)
		if err != nil {
			return err
		}
		documents = append(documents, parsed...)
	}
	if err := validateRenderedPolicies(traffic, documents, paths.googleEnabled); err != nil {
		return err
	}
	return validateRenderedWorkloads(workloads, documents)
}

func readManifests(path string) ([]manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	documents := []manifest{}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	for {
		document := make(manifest)
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if len(document) > 0 {
			documents = append(documents, document)
		}
	}
	return documents, nil
}

func validateRenderedPolicies(matrix trafficMatrix, documents []manifest, googleEnabled bool) error { //nolint:revive // The explicit auth variant selects one policy matrix.
	expectedIDs := []string{}
	expectedPolicies := []string{}
	expectedPolicyIDs := make(map[string][]string)
	for _, row := range matrix.Rows {
		if row.Environment != "all" && (!googleEnabled || row.Environment != environmentGoogleEnabled) {
			continue
		}
		expectedIDs = append(expectedIDs, row.ID)
		expectedPolicyIDs[row.PolicyName] = append(expectedPolicyIDs[row.PolicyName], row.ID)
		if !slices.Contains(expectedPolicies, row.PolicyName) {
			expectedPolicies = append(expectedPolicies, row.PolicyName)
		}
	}

	actualIDs := []string{}
	actualPolicies := []string{}
	for _, document := range documents {
		if stringValue(document, "kind") != "NetworkPolicy" {
			continue
		}
		name := nestedString(document, "metadata", "name")
		actualPolicies = append(actualPolicies, name)
		ids := nestedString(document, "metadata", "annotations", "vermouth.dev/traffic-row-ids")
		if ids == "" {
			return fmt.Errorf("NetworkPolicy %q has no traffic row annotation", name)
		}
		policyIDs := strings.Split(ids, ",")
		if !slices.Equal(policyIDs, expectedPolicyIDs[name]) {
			return fmt.Errorf("NetworkPolicy %q traffic row order is %v, expected %v", name, policyIDs, expectedPolicyIDs[name])
		}
		actualIDs = append(actualIDs, policyIDs...)
	}
	slices.Sort(expectedIDs)
	slices.Sort(expectedPolicies)
	slices.Sort(actualIDs)
	slices.Sort(actualPolicies)
	if !slices.Equal(actualIDs, expectedIDs) {
		return fmt.Errorf("rendered traffic row IDs differ: got %v, want %v", actualIDs, expectedIDs)
	}
	if !slices.Equal(actualPolicies, expectedPolicies) {
		return fmt.Errorf("rendered NetworkPolicies differ: got %v, want %v", actualPolicies, expectedPolicies)
	}
	return nil
}

func validateRenderedWorkloads(matrix workloadMatrix, documents []manifest) error {
	counts := make(map[string]int)
	for _, document := range documents {
		category, component := renderedWorkloadCategory(document)
		if category == "" {
			continue
		}
		expected, ok := matrix.Workloads[category]
		if !ok {
			return fmt.Errorf("rendered component %q uses unknown workload category %q", component, category)
		}
		if err := validateRenderedWorkload(document, component, expected); err != nil {
			return err
		}
		counts[category]++
	}
	expectedCounts := map[string]int{
		workloadGoApplication:    applicationWorkloadCount,
		workloadWeb:              1,
		workloadPostgres:         serviceCount,
		workloadRedpanda:         1,
		workloadGarage:           1,
		workloadMigration:        serviceCount,
		workloadGarageInit:       1,
		workloadDevelopmentToken: 1,
	}
	for category, expected := range expectedCounts {
		if counts[category] != expected {
			return fmt.Errorf("rendered %s count is %d, expected %d", category, counts[category], expected)
		}
	}
	return nil
}

func renderedWorkloadCategory(document manifest) (category, component string) {
	kind := stringValue(document, "kind")
	if kind != "Deployment" && kind != "StatefulSet" && kind != kindJob {
		return "", ""
	}
	component = nestedString(document, "metadata", "labels", "app.kubernetes.io/component")
	switch {
	case component == workloadWeb:
		return workloadWeb, component
	case component == "gateway" ||
		component == productionServiceIdentity ||
		component == productionServiceTeaching ||
		component == productionServiceBilling ||
		component == productionServiceNotifications:
		return workloadGoApplication, component
	case strings.HasPrefix(component, "postgres-"):
		return workloadPostgres, component
	case component == workloadRedpanda ||
		component == workloadGarage ||
		component == workloadGarageInit ||
		component == workloadDevelopmentToken:
		return component, component
	case strings.HasPrefix(component, "migrate-"):
		return workloadMigration, component
	default:
		return "", ""
	}
}

func validateRenderedWorkload(document manifest, component string, expected workloadEntry) error {
	kind := stringValue(document, "kind")
	if kind != expected.Kind {
		return fmt.Errorf("%s %q kind is %s, expected %s", kind, component, kind, expected.Kind)
	}
	if kind != kindJob && nestedInt(document, "spec", "replicas") != expected.Replicas {
		return fmt.Errorf("%s %q replicas differ from workload matrix", kind, component)
	}
	if kind == kindJob && (nestedInt(document, "spec", "activeDeadlineSeconds") != expected.ActiveDeadlineSeconds ||
		nestedInt(document, "spec", "backoffLimit") != expected.BackoffLimit) {
		return fmt.Errorf("Job %q deadline or backoff differs from workload matrix", component)
	}
	podSpec := nestedMap(document, "spec", "template", "spec")
	if stringValue(podSpec, "serviceAccountName") != resolvedServiceAccount(expected.ServiceAccount, component) {
		return fmt.Errorf("%s %q ServiceAccount differs from workload matrix", kind, component)
	}
	if !hasExactBool(podSpec, "automountServiceAccountToken", expected.AutomountServiceAccountToken) {
		return fmt.Errorf("%s %q token mount differs from workload matrix", kind, component)
	}
	security := mapValue(podSpec, "securityContext")
	if !hasExactBool(security, "runAsNonRoot", true) ||
		intValue(security, "runAsUser") != expected.RunAsUser ||
		intValue(security, "runAsGroup") != expected.RunAsGroup ||
		nestedString(security, "seccompProfile", "type") != "RuntimeDefault" {
		return fmt.Errorf("%s %q Pod security differs from workload matrix", kind, component)
	}
	if expected.FSGroup != 0 && intValue(security, "fsGroup") != expected.FSGroup {
		return fmt.Errorf("%s %q fsGroup differs from workload matrix", kind, component)
	}

	containers := sliceValue(podSpec, "containers")
	if len(containers) != 1 {
		return fmt.Errorf("%s %q must render exactly one container", kind, component)
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return fmt.Errorf("%s %q container is invalid", kind, component)
	}
	containerSecurity := mapValue(container, "securityContext")
	if !hasExactBool(containerSecurity, "allowPrivilegeEscalation", false) ||
		!hasExactBool(containerSecurity, "readOnlyRootFilesystem", expected.ReadOnlyRootFilesystem) ||
		!slices.Equal(stringSlice(nestedValue(containerSecurity, "capabilities", "drop")), []string{"ALL"}) {
		return fmt.Errorf("%s %q container security differs from workload matrix", kind, component)
	}
	if err := compareResources(component, expected, container); err != nil {
		return err
	}
	if err := compareProbe(component, "readiness", expected.Readiness, mapValue(container, "readinessProbe")); err != nil {
		return err
	}
	if err := compareProbe(component, "liveness", expected.Liveness, mapValue(container, "livenessProbe")); err != nil {
		return err
	}
	return compareWritableMounts(component, expected, podSpec, container)
}

func compareProbe(component, name string, expected *workloadProbe, actual map[string]any) error {
	if expected == nil {
		if len(actual) != 0 {
			return fmt.Errorf("%s has an unexpected %s probe", component, name)
		}
		return nil
	}
	if intValue(actual, "periodSeconds") != expected.PeriodSeconds ||
		intValue(actual, "timeoutSeconds") != expected.TimeoutSeconds ||
		intValue(actual, "failureThreshold") != expected.FailureThreshold {
		return fmt.Errorf("%s %s probe timing differs from workload matrix", component, name)
	}
	switch expected.Type {
	case "http":
		if nestedString(actual, "httpGet", "path") != expected.Path {
			return fmt.Errorf("%s %s probe path differs from workload matrix", component, name)
		}
	case "exec":
		command := stringSlice(nestedValue(actual, "exec", "command"))
		wanted := make([]string, 0, len(expected.Command))
		role := "vermouth_" + strings.TrimPrefix(component, "postgres-")
		for _, part := range expected.Command {
			wanted = append(wanted, strings.ReplaceAll(part, "{role}", role))
		}
		if !slices.Equal(command, wanted) {
			return fmt.Errorf("%s %s probe command differs from workload matrix", component, name)
		}
	default:
		return fmt.Errorf("%s has unknown %s probe type %q", component, name, expected.Type)
	}
	return nil
}

func resolvedServiceAccount(value, component string) string {
	if value == "component" {
		return component
	}
	return value
}

func compareResources(component string, expected workloadEntry, container map[string]any) error {
	requests := nestedMap(container, "resources", "requests")
	limits := nestedMap(container, "resources", "limits")
	if len(requests) != len(expected.Resources.Requests) || len(limits) != len(expected.Resources.Limits) {
		return fmt.Errorf("%s has extra or missing resource requests or limits", component)
	}
	for resource, value := range expected.Resources.Requests {
		if fmt.Sprint(requests[resource]) != value {
			return fmt.Errorf("%s request %s is %v, expected %s", component, resource, requests[resource], value)
		}
	}
	for resource, value := range expected.Resources.Limits {
		if fmt.Sprint(limits[resource]) != value {
			return fmt.Errorf("%s limit %s is %v, expected %s", component, resource, limits[resource], value)
		}
	}
	return nil
}

func compareWritableMounts(
	component string,
	expected workloadEntry,
	podSpec map[string]any,
	container map[string]any,
) error {
	mounts := sliceValue(container, "volumeMounts")
	volumes := sliceValue(podSpec, "volumes")
	writableCount := 0
	for _, value := range mounts {
		mount, ok := value.(map[string]any)
		if ok && !boolValue(mount, "readOnly") {
			writableCount++
		}
	}
	if writableCount != len(expected.WritableMounts) {
		return fmt.Errorf("%s has %d writable mounts, expected %d", component, writableCount, len(expected.WritableMounts))
	}
	for _, wanted := range expected.WritableMounts {
		if !containsNamedPath(mounts, wanted.Name, wanted.MountPath) {
			return fmt.Errorf("%s is missing writable mount %s at %s", component, wanted.Name, wanted.MountPath)
		}
		if wanted.Medium == "Persistent" {
			continue
		}
		volume := findNamed(volumes, wanted.Name)
		emptyDir := mapValue(volume, "emptyDir")
		medium := fmt.Sprint(emptyDir["medium"])
		if wanted.Medium == "Disk" {
			medium = "Disk"
		}
		if medium != wanted.Medium || fmt.Sprint(emptyDir["sizeLimit"]) != wanted.SizeLimit {
			return fmt.Errorf("%s writable volume %s differs from workload matrix", component, wanted.Name)
		}
	}
	return nil
}

func containsNamedPath(values []any, name, path string) bool {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if ok && stringValue(object, "name") == name && stringValue(object, "mountPath") == path {
			return true
		}
	}
	return false
}

func findNamed(values []any, name string) map[string]any {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if ok && stringValue(object, "name") == name {
			return object
		}
	}
	return make(map[string]any)
}

func nestedValue(value map[string]any, path ...string) any {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func nestedMap(value map[string]any, path ...string) map[string]any {
	object, _ := nestedValue(value, path...).(map[string]any)
	if object == nil {
		return make(map[string]any)
	}
	return object
}

func nestedString(value map[string]any, path ...string) string {
	return fmt.Sprint(nestedValue(value, path...))
}

func nestedInt(value map[string]any, path ...string) int {
	return intValue(nestedMap(value, path[:len(path)-1]...), path[len(path)-1])
}

func mapValue(value map[string]any, key string) map[string]any {
	object, _ := value[key].(map[string]any)
	if object == nil {
		return make(map[string]any)
	}
	return object
}

func sliceValue(value map[string]any, key string) []any {
	items, _ := value[key].([]any)
	if items == nil {
		return []any{}
	}
	return items
}

func stringValue(value map[string]any, key string) string {
	return fmt.Sprint(value[key])
}

func intValue(value map[string]any, key string) int {
	switch number := value[key].(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

func boolValue(value map[string]any, key string) bool {
	boolean, _ := value[key].(bool)
	return boolean
}

func hasExactBool(value map[string]any, key string, wanted bool) bool {
	boolean, ok := value[key].(bool)
	return ok && boolean == wanted
}

func stringSlice(value any) []string {
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, fmt.Sprint(item))
	}
	return result
}
