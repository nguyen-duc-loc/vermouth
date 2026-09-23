package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCIAppliesMigrationsBeforeIntegrationTests keeps the live schema gate from
// running against the empty Postgres databases created for a fresh CI runner.
func TestCIAppliesMigrationsBeforeIntegrationTests(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	require.NoError(t, err)

	workflow := string(body)
	require.Contains(t, workflow, "go install github.com/pressly/goose/v3/cmd/goose@v3.27.3")

	infraIndex := strings.Index(workflow, "- name: Start test infrastructure")
	migrationIndex := strings.Index(workflow, "- name: Apply migrations")
	testIndex := strings.Index(workflow, "- name: Test")
	require.NotEqual(t, -1, infraIndex)
	require.NotEqual(t, -1, migrationIndex)
	require.NotEqual(t, -1, testIndex)
	require.Less(t, infraIndex, migrationIndex)
	require.Less(t, migrationIndex, testIndex)

	migrationStep := workflow[migrationIndex:testIndex]
	require.Contains(t, migrationStep, "run: task migrate:up")
	for _, variable := range []string{
		"IDENTITY_DATABASE_URL",
		"TEACHING_DATABASE_URL",
		"BILLING_DATABASE_URL",
		"NOTIFICATIONS_DATABASE_URL",
	} {
		require.Contains(t, migrationStep, variable)
	}
	testStep := workflow[testIndex:]
	for _, variable := range []string{
		"IDENTITY_DATABASE_URL",
		"TEACHING_DATABASE_URL",
		"BILLING_DATABASE_URL",
		"NOTIFICATIONS_DATABASE_URL",
	} {
		require.Contains(t, strings.Split(testStep, "  web:")[0], variable)
	}
	require.Contains(t, workflow, "pnpm --filter web exec vitest run")
}
