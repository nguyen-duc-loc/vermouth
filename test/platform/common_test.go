package platform_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-17
func TestVersionAtLeastComparesNumericSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		actual  string
		minimum string
		wantOK  bool
	}{
		{name: "same version", actual: "1.36.3", minimum: "1.36.3", wantOK: true},
		{name: "newer patch", actual: "1.36.10", minimum: "1.36.3", wantOK: true},
		{name: "newer minor", actual: "1.37.0", minimum: "1.36.3", wantOK: true},
		{name: "newer fourth segment", actual: "1.36.3.2", minimum: "1.36.3.1", wantOK: true},
		{name: "older patch", actual: "1.36.2", minimum: "1.36.3"},
		{name: "older fourth segment", actual: "1.36.3.1", minimum: "1.36.3.2"},
		{name: "older major", actual: "0.99.0", minimum: "1.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := runSourcedShell(
				t,
				[]string{repoFile(t, "deploy", "platform", "common.sh")},
				"version_at_least \"$ACTUAL\" \"$MINIMUM\"\n",
				"",
				map[string]string{"ACTUAL": test.actual, "MINIMUM": test.minimum},
			)
			if test.wantOK {
				require.NoError(t, result.err, result.stderr)
				return
			}
			require.Error(t, result.err)
		})
	}
}

// covers: AC-1, AC-17
func TestRequireSeriesAcceptsOnlyTheSupportedSeriesAtTheMinimum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		actual    string
		series    string
		minimum   string
		wantError string
	}{
		{name: "bare supported series", actual: "1.36", series: "1.36", minimum: "1.36.0"},
		{name: "newer supported patch", actual: "1.36.9", series: "1.36", minimum: "1.36.3"},
		{
			name:      "different series",
			actual:    "1.37.0",
			series:    "1.36",
			minimum:   "1.36.3",
			wantError: "outside supported series 1.36.x",
		},
		{
			name:      "older supported patch",
			actual:    "1.36.2",
			series:    "1.36",
			minimum:   "1.36.3",
			wantError: "older than 1.36.3",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := runSourcedShell(
				t,
				[]string{repoFile(t, "deploy", "platform", "common.sh")},
				"require_series tool \"$ACTUAL\" \"$SERIES\" \"$MINIMUM\"\n",
				"",
				map[string]string{
					"ACTUAL":  test.actual,
					"SERIES":  test.series,
					"MINIMUM": test.minimum,
				},
			)
			if test.wantError == "" {
				require.NoError(t, result.err, result.stderr)
				require.Contains(t, result.stdout, test.actual)
				return
			}
			require.Error(t, result.err)
			require.Contains(t, result.stderr, test.wantError)
		})
	}
}

// covers: AC-5, AC-8, AC-12
func TestExactConfirmationAcceptsOnlyVermouth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantCode int
	}{
		{name: "exact confirmation", input: "vermouth\n"},
		{name: "wrong confirmation", input: "Vermouth\n", wantCode: 8},
		{name: "empty confirmation", input: "\n", wantCode: 8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := runSourcedShell(
				t,
				[]string{repoFile(t, "deploy", "platform", "common.sh")},
				"confirm_exact_vermouth \"Delete local data\"\n",
				test.input,
				nil,
			)
			if test.wantCode == 0 {
				require.NoError(t, result.err, result.stderr)
				return
			}
			require.Equal(t, test.wantCode, exitCode(t, result.err))
			require.Contains(t, result.stderr, "nothing changed")
		})
	}
}
