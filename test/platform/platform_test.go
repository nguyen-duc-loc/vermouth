package platform_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func TestPlatformRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	result := runCommand(
		t,
		"",
		nil,
		[]string{repoFile(t, "deploy", "platform", "platform.sh"), "unknown"},
	)

	require.Error(t, result.err)
	require.Equal(t, 1, exitCode(t, result.err))
	require.Contains(t, result.stderr, "platform: unknown command unknown")
}

func TestPlatformRejectsAMissingCommand(t *testing.T) {
	t.Parallel()

	result := runCommand(
		t,
		"",
		nil,
		[]string{repoFile(t, "deploy", "platform", "platform.sh")},
	)

	require.Error(t, result.err)
	require.Equal(t, 1, exitCode(t, result.err))
	require.Contains(t, result.stderr, "platform: unknown command")
}

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return filepath.Join(append([]string{root}, parts...)...)
}

func runSourcedShell(
	t *testing.T,
	sources []string,
	body string,
	input string,
	overrides map[string]string,
) commandResult {
	t.Helper()
	require.NotEmpty(t, sources)

	var script strings.Builder
	for index := range sources {
		_, err := fmt.Fprintf(&script, ". \"$%d\"\n", index+1)
		require.NoError(t, err)
	}
	script.WriteString(body)

	arguments := make([]string, 0, 3+len(sources))
	arguments = append(arguments, "-c", script.String(), sources[0])
	arguments = append(arguments, sources...)
	return runCommand(t, input, overrides, arguments)
}

func runCommand(
	t *testing.T,
	input string,
	overrides map[string]string,
	arguments []string,
) commandResult {
	t.Helper()

	command := exec.CommandContext(t.Context(), "/bin/sh", arguments...)
	command.Dir = repoFile(t)
	command.Env = mergedEnvironment(overrides)
	command.Stdin = strings.NewReader(input)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func mergedEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	maps.Copy(values, overrides)

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func writeExecutable(t *testing.T, directory, name, body string) {
	t.Helper()

	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700))
}

func exitCode(t *testing.T, err error) int {
	t.Helper()

	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	return exitError.ExitCode()
}
