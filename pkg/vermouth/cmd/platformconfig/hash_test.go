package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-2, AC-7, AC-8, AC-17
func TestWriteImagePlan(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	require.NoError(t, err)

	tests := []struct {
		name      string
		workload  string
		mode      string
		wantParts []string
		wantError string
	}{
		{
			name:     "native gateway",
			workload: "gateway",
			mode:     "native",
			wantParts: []string{
				`"repository_path":"vermouth/gateway"`,
				`"platforms":["linux/arm64"]`,
				`"tag":"dev-`,
			},
		},
		{
			name:     "multiple architecture web",
			workload: "web",
			mode:     "multi",
			wantParts: []string{
				`"repository_path":"vermouth/web"`,
				`"platforms":["linux/arm64","linux/amd64"]`,
				`"tag":"multi-`,
			},
		},
		{
			name:      "development token has no multiple architecture output",
			workload:  "devtoken",
			mode:      "multi",
			wantError: "has no multi output",
		},
		{
			name:      "unknown workload",
			workload:  "unknown",
			mode:      "native",
			wantError: "unknown workload",
		},
		{
			name:      "unknown mode",
			workload:  "gateway",
			mode:      "staged",
			wantError: "unknown image plan mode",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			err := writeImagePlan(root, test.workload, test.mode, &output)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			for _, part := range test.wantParts {
				require.Contains(t, output.String(), part)
			}
		})
	}
}

// covers: AC-2, AC-11
func TestWriteSecretHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		want      string
		wantError string
	}{
		{
			name:  "sorts keys by UTF8 bytes",
			input: `{"B":"two","A":"one"}`,
			want:  "ee560e6ac1bd06f89c8ce1752f7a2fd553a8120ca8dade6f7873bde5e0bc1d5c\n",
		},
		{
			name:      "rejects empty values",
			input:     `{}`,
			wantError: "must not be empty",
		},
		{
			name:      "rejects a second document",
			input:     `{"A":"one"} {"B":"two"}`,
			wantError: "one JSON object",
		},
		{
			name:      "rejects malformed JSON",
			input:     `{"A":`,
			wantError: "decode secret values",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			err := writeSecretHash(strings.NewReader(test.input), &output)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, output.String())
			require.NotContains(t, output.String(), "one")
			require.NotContains(t, output.String(), "two")
		})
	}
}

// covers: AC-2, AC-7, AC-8, AC-11, AC-17
func TestCanonicalImageHash_ChangesForEachContractInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*testing.T, *imageHashFixture)
	}{
		{
			name: "input content",
			mutate: func(t *testing.T, fixture *imageHashFixture) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "input.txt"), []byte("changed"), 0o600))
			},
		},
		{
			name: "input permission bits",
			mutate: func(t *testing.T, fixture *imageHashFixture) {
				t.Helper()
				require.NoError(t, os.Chmod(filepath.Join(fixture.root, "input.txt"), 0o640))
			},
		},
		{
			name: "Dockerfile content",
			mutate: func(t *testing.T, fixture *imageHashFixture) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "Dockerfile"), []byte("FROM scratch\nLABEL changed=true\n"), 0o600))
			},
		},
		{
			name: "Docker ignore presence",
			mutate: func(t *testing.T, fixture *imageHashFixture) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".dockerignore"), []byte(".tmp\n"), 0o600))
			},
		},
		{
			name: "build target",
			mutate: func(_ *testing.T, fixture *imageHashFixture) {
				fixture.image.Target = "alternate"
			},
		},
		{
			name: "platform collection",
			mutate: func(_ *testing.T, fixture *imageHashFixture) {
				fixture.platforms = append(fixture.platforms, platformAMD64)
			},
		},
		{
			name: "build argument",
			mutate: func(_ *testing.T, fixture *imageHashFixture) {
				fixture.image.BuildArgs["VERSION"] = "2"
			},
		},
		{
			name: "base provenance",
			mutate: func(_ *testing.T, fixture *imageHashFixture) {
				fixture.external["scratch"] = externalImage{Digest: "sha256:changed"}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newImageHashFixture(t)
			before, err := canonicalImageHash(
				fixture.root,
				fixture.image,
				fixture.external,
				fixture.platforms,
			)
			require.NoError(t, err)

			test.mutate(t, &fixture)
			after, err := canonicalImageHash(
				fixture.root,
				fixture.image,
				fixture.external,
				fixture.platforms,
			)
			require.NoError(t, err)
			require.NotEqual(t, before, after)
		})
	}
}

// covers: AC-2, AC-7, AC-17
func TestCollectInputFiles_SortsFilesAndIgnoresGeneratedDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, directory := range []string{"app/src", "app/.git", "app/.tmp", "app/.vite", "app/dist", "app/node_modules"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, directory), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "z.go"), []byte("z"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "src", "a.go"), []byte("a"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", ".git", "ignored"), []byte("git"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", ".tmp", "ignored"), []byte("tmp"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", ".vite", "ignored"), []byte("vite"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "dist", "ignored"), []byte("dist"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "node_modules", "ignored"), []byte("node"), 0o600))

	files, err := collectInputFiles(root, []string{"app"})
	require.NoError(t, err)
	require.Equal(t, []string{"app/src/a.go", "app/z.go"}, []string{files[0].path, files[1].path})
	require.Equal(t, os.FileMode(0o640), files[0].mode.Perm())
}

// covers: AC-2, AC-7, AC-17
func TestCollectInputFilesRejectsSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.txt"), []byte("target"), 0o600))
	require.NoError(t, os.Symlink("target.txt", filepath.Join(root, "input.txt")))

	_, err := collectInputFiles(root, []string{"input.txt"})
	require.ErrorContains(t, err, "is a symlink")
}

// covers: AC-2, AC-7, AC-8, AC-17
func TestCanonicalImageHashIsStableAcrossUnorderedContractInputs(t *testing.T) {
	t.Parallel()

	fixture := newImageHashFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "second.txt"), []byte("second"), 0o600))
	fixture.image.Inputs = []string{"input.txt", "second.txt"}
	fixture.image.BuildArgs = map[string]string{"VERSION": "1", "CHANNEL": "stable"}
	fixture.image.BaseLocks = []string{"scratch", "toolchain"}
	fixture.external["toolchain"] = externalImage{Digest: "sha256:toolchain"}

	first, err := canonicalImageHash(fixture.root, fixture.image, fixture.external, fixture.platforms)
	require.NoError(t, err)

	fixture.image.Inputs = []string{"second.txt", "input.txt"}
	fixture.image.BuildArgs = map[string]string{"CHANNEL": "stable", "VERSION": "1"}
	fixture.image.BaseLocks = []string{"toolchain", "scratch"}
	second, err := canonicalImageHash(fixture.root, fixture.image, fixture.external, fixture.platforms)
	require.NoError(t, err)

	require.Equal(t, first, second)
}

func TestBaseProvenance_SortsLockNames(t *testing.T) {
	t.Parallel()

	image := builtImage{BaseLocks: []string{"z-base", "a-base"}}
	external := map[string]externalImage{
		"a-base": {Digest: "sha256:a"},
		"z-base": {Digest: "sha256:z"},
	}

	require.Equal(t, "a-base=sha256:a,z-base=sha256:z", baseProvenance(image, external))
}

type imageHashFixture struct {
	root      string
	image     builtImage
	external  map[string]externalImage
	platforms []string
}

func newImageHashFixture(t *testing.T) imageHashFixture {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "input.txt"), []byte("input"), 0o600))
	return imageHashFixture{
		root: root,
		image: builtImage{
			Dockerfile: "Dockerfile",
			Target:     "runtime",
			Inputs:     []string{"input.txt"},
			BuildArgs:  map[string]string{"VERSION": "1"},
			BaseLocks:  []string{"scratch"},
			HashSchema: "vermouth-image-v1",
		},
		external: map[string]externalImage{
			"scratch": {Digest: "sha256:original"},
		},
		platforms: []string{platformARM64},
	}
}
