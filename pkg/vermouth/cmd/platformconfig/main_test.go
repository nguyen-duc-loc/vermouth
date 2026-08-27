package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-17
func TestRunValidatesTheRepositoryInputs(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	require.NoError(t, err)
	require.NoError(t, run([]string{"validate-inputs", root}))
}

// covers: AC-11, AC-17
func TestRunRejectsInvalidCommandArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{name: "missing command", wantError: "expected validate-inputs"},
		{name: "validate inputs", args: []string{"validate-inputs"}, wantError: "usage: platformconfig validate-inputs"},
		{name: "get", args: []string{"get", "file"}, wantError: "usage: platformconfig get"},
		{name: "image plan", args: []string{"image-plan", "root", "gateway"}, wantError: "usage: platformconfig image-plan"},
		{name: "validate document", args: []string{"validate-document", "schema"}, wantError: "usage: platformconfig validate-document"},
		{name: "secret hash", args: []string{"secret-hash", "extra"}, wantError: "usage: platformconfig secret-hash"},
		{name: "atomic commit", args: []string{"atomic-commit", "next"}, wantError: "usage: platformconfig atomic-commit"},
		{name: "validate rendered", args: []string{"validate-rendered", "traffic"}, wantError: "usage: platformconfig validate-rendered"},
		{name: "unknown command", args: []string{"unknown"}, wantError: `unknown command "unknown"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := run(test.args)
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

// covers: AC-9, AC-10, AC-17
func TestParseRenderedPathsAcceptsOnlyKnownEnvironments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantGoogle bool
		wantError  string
	}{
		{
			name: "local environment",
			args: []string{"validate-rendered", "traffic", "workloads", "foundation", "application", "jobs"},
		},
		{
			name:       "Google enabled environment",
			args:       []string{"validate-rendered", "traffic", "workloads", "foundation", "application", "jobs", environmentGoogleEnabled},
			wantGoogle: true,
		},
		{
			name:      "unknown environment",
			args:      []string{"validate-rendered", "traffic", "workloads", "foundation", "application", "jobs", "production"},
			wantError: "unknown rendered environment",
		},
		{name: "missing paths", args: []string{"validate-rendered"}, wantError: "usage: platformconfig validate-rendered"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			paths, err := parseRenderedPaths(test.args)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "traffic", paths.traffic)
			require.Equal(t, "workloads", paths.workloads)
			require.Equal(t, "foundation", paths.foundation)
			require.Equal(t, "application", paths.applications)
			require.Equal(t, "jobs", paths.jobs)
			require.Equal(t, test.wantGoogle, paths.googleEnabled)
		})
	}
}
