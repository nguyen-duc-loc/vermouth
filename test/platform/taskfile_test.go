package platform_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-6, AC-13
func TestTaskfileKeepsPublicStartupCommandsStable(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(repoFile(t, "Taskfile.yml"))
	require.NoError(t, err)

	require.Equal(t,
		[]string{"./deploy/platform/platform.sh dev"},
		taskCommands(string(content), "dev"),
	)
	require.Equal(t,
		[]string{"./test/thread.sh"},
		taskCommands(string(content), "thread"),
	)
}

func taskCommands(content, task string) []string {
	lines := strings.Split(content, "\n")
	inside := false
	commands := []string{}
	for _, line := range lines {
		if line == "  "+task+":" {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			break
		}
		if inside && strings.HasPrefix(line, "      - ") {
			commands = append(commands, strings.TrimPrefix(line, "      - "))
		}
	}
	return commands
}
