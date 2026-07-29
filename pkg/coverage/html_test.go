// Copyright 2026 Paul Greenberg greenpau@outlook.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package coverage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGenerateHTMLWithGoToolCover(t *testing.T) {
	goCommand, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go executable is unavailable: %v", err)
	}

	projectDir := t.TempDir()
	source := "package sample\n\nfunc Answer() int { return 42 }\n"
	if err := os.WriteFile(filepath.Join(projectDir, "sample.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectDir, "go.mod"),
		[]byte("module example.com/sample\n\ngo 1.25.0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(projectDir, "evidence", "coverage.out")
	if err := os.Mkdir(filepath.Dir(profilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	profile := "mode: set\nexample.com/sample/sample.go:3.1,3.32 1 1\n"
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join("reports", "coverage.html")
	if err := GenerateHTML(context.Background(), HTMLOptions{
		GoCommand:  goCommand,
		ProjectDir: projectDir,
		ProfilePath: filepath.Join(
			"evidence",
			"coverage.out",
		),
		OutputPath: outputPath,
		Decorate:   decorateCoverageHTMLForTest,
	}); err != nil {
		t.Fatalf("GenerateHTML() error = %v", err)
	}

	resolvedOutput := filepath.Join(projectDir, outputPath)
	data, err := os.ReadFile(resolvedOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Go Coverage Report") ||
		!strings.Contains(string(data), "sample.go") ||
		!strings.Contains(string(data), `id="test-coverage-theme"`) {
		t.Fatalf("generated HTML does not look canonical: %q", data)
	}
	assertPrivateFile(t, resolvedOutput)
}

func TestGenerateHTMLUsesSelectedCommandAndProjectDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}

	projectDir := t.TempDir()
	canonicalProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, projectDir, "fake-go", fmt.Sprintf(`#!/bin/sh
if [ "$PWD" != %s ]; then
  echo "wrong working directory: $PWD" >&2
  exit 20
fi
if [ "$1" != "tool" ] || [ "$2" != "cover" ]; then
  echo "wrong arguments: $*" >&2
  exit 21
fi
for arg in "$@"; do
  case "$arg" in
    -o=*) output=${arg#-o=} ;;
  esac
done
if [ -z "$output" ]; then
  echo "missing output argument" >&2
  exit 22
fi
printf '<html>selected command</html>' > "$output"
`, shellQuote(canonicalProjectDir)))

	if err := os.WriteFile(filepath.Join(projectDir, "coverage.out"), []byte("mode: set\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "coverage.html"), []byte("old report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := GenerateHTML(context.Background(), HTMLOptions{
		GoCommand:   script,
		ProjectDir:  projectDir,
		ProfilePath: "coverage.out",
		OutputPath:  "coverage.html",
	}); err != nil {
		t.Fatalf("GenerateHTML() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "coverage.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>selected command</html>" {
		t.Fatalf("output = %q, want selected command output", data)
	}
}

func TestDecorateGeneratedHTMLPreservesCanonicalOnFailure(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".coverage.html.tmp-source")
	canonical := []byte("<html><head></head><body>canonical</body></html>")
	if err := os.WriteFile(path, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := secureGeneratedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decorateErr := errors.New("decoration failed")
	err = decorateGeneratedHTML(
		context.Background(),
		path,
		snapshot,
		func(context.Context, io.Reader, io.Writer) error {
			return decorateErr
		},
	)
	if !errors.Is(err, decorateErr) {
		t.Fatalf("decorateGeneratedHTML() error = %v, want %v", err, decorateErr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, canonical) {
		t.Fatalf("canonical HTML changed after decorator failure: %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(
		dir,
		"..coverage.html.tmp-source.theme-*",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("decorated temporary files remain: %v", matches)
	}
}

func TestGenerateHTMLRejectsGeneratedPathChangesBeforeInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}

	tests := []struct {
		name   string
		attack func(*testing.T, string, generatedFileSnapshot)
	}{
		{
			name: "path_replacement_with_restored_metadata",
			attack: func(
				t *testing.T,
				path string,
				expected generatedFileSnapshot,
			) {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				replacementPath := filepath.Join(
					filepath.Dir(path),
					".generated-replacement",
				)
				originalPath := filepath.Join(
					filepath.Dir(path),
					".generated-original",
				)
				if err := os.WriteFile(
					replacementPath,
					data,
					expected.mode.Perm(),
				); err != nil {
					t.Fatal(err)
				}
				restoreGeneratedSnapshotMetadata(
					t,
					replacementPath,
					expected,
				)
				if err := os.Rename(path, originalPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacementPath, path); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(originalPath); err != nil {
					t.Fatal(err)
				}
				assertGeneratedSnapshotMetadata(
					t,
					path,
					expected,
					false,
				)
				if got := sha256.Sum256(data); got != expected.digest {
					t.Fatalf(
						"replacement digest = %x, want %x",
						got,
						expected.digest,
					)
				}
			},
		},
		{
			name: "same_object_rewrite_with_restored_metadata",
			attack: func(
				t *testing.T,
				path string,
				expected generatedFileSnapshot,
			) {
				t.Helper()
				changed := []byte("tampered report!")
				if int64(len(changed)) != expected.size {
					t.Fatalf(
						"test attack size = %d, want %d",
						len(changed),
						expected.size,
					)
				}
				if err := os.WriteFile(
					path,
					changed,
					expected.mode.Perm(),
				); err != nil {
					t.Fatal(err)
				}
				restoreGeneratedSnapshotMetadata(t, path, expected)
				assertGeneratedSnapshotMetadata(
					t,
					path,
					expected,
					true,
				)
				if got := sha256.Sum256(changed); got == expected.digest {
					t.Fatal("changed contents unexpectedly retained digest")
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectDir := t.TempDir()
			script := writeScript(
				t,
				projectDir,
				"fake-go",
				`#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    -o=*) output=${arg#-o=} ;;
  esac
done
printf 'generated report' > "$output"
`,
			)
			writeCoverageProfile(
				t,
				filepath.Join(projectDir, "coverage.out"),
			)
			outputPath := filepath.Join(projectDir, "coverage.html")
			oldOutput := []byte("prior report")
			if err := os.WriteFile(outputPath, oldOutput, 0o600); err != nil {
				t.Fatal(err)
			}

			hookCalled := false
			err := generateHTML(
				context.Background(),
				HTMLOptions{
					GoCommand:   script,
					ProjectDir:  projectDir,
					ProfilePath: "coverage.out",
					OutputPath:  "coverage.html",
				},
				func(
					path string,
					snapshot generatedFileSnapshot,
				) error {
					hookCalled = true
					test.attack(t, path, snapshot)
					return nil
				},
			)
			if !hookCalled {
				t.Fatal("generated-file attack hook was not called")
			}
			if !errors.Is(err, errGeneratedFileChanged) {
				t.Fatalf(
					"GenerateHTML() error = %v, want "+
						"errGeneratedFileChanged",
					err,
				)
			}
			gotOutput, readErr := os.ReadFile(outputPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(gotOutput) != string(oldOutput) {
				t.Fatalf(
					"destination = %q, want preserved %q",
					gotOutput,
					oldOutput,
				)
			}
			assertNoTemporaryFiles(t, projectDir)
		})
	}
}

func TestGenerateHTMLPreservesDestinationOnCommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}

	projectDir := t.TempDir()
	script := writeScript(t, projectDir, "failing-go", `#!/bin/sh
echo "cover helper exploded" >&2
exit 17
`)
	output := filepath.Join(projectDir, "coverage.html")
	writeCoverageProfile(t, filepath.Join(projectDir, "coverage.out"))
	if err := os.WriteFile(output, []byte("prior report"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := GenerateHTML(context.Background(), HTMLOptions{
		GoCommand:   script,
		ProjectDir:  projectDir,
		ProfilePath: "coverage.out",
		OutputPath:  "coverage.html",
	})
	if err == nil {
		t.Fatal("GenerateHTML() error = nil, want command failure")
	}
	if !strings.Contains(err.Error(), "cover helper exploded") {
		t.Fatalf("GenerateHTML() error = %v, want stderr detail", err)
	}
	data, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "prior report" {
		t.Fatalf("destination = %q, want prior report", data)
	}
	assertNoTemporaryFiles(t, projectDir)
}

func TestGenerateHTMLCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}

	projectDir := t.TempDir()
	script := writeScript(t, projectDir, "blocking-go", `#!/bin/sh
exec sleep 30
`)
	output := filepath.Join(projectDir, "coverage.html")
	writeCoverageProfile(t, filepath.Join(projectDir, "coverage.out"))
	if err := os.WriteFile(output, []byte("prior report"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := GenerateHTML(ctx, HTMLOptions{
		GoCommand:   script,
		ProjectDir:  projectDir,
		ProfilePath: "coverage.out",
		OutputPath:  "coverage.html",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GenerateHTML() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("GenerateHTML() cancellation took %s", elapsed)
	}
	data, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "prior report" {
		t.Fatalf("destination = %q, want prior report", data)
	}
	assertNoTemporaryFiles(t, projectDir)
}

func TestGenerateHTMLValidationAndUnsafeTargets(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		opts HTMLOptions
	}{
		{name: "nil context"},
		{name: "cancelled context", ctx: cancelledContext(), opts: HTMLOptions{ProjectDir: "x", ProfilePath: "x", OutputPath: "x"}},
		{name: "missing project", ctx: context.Background(), opts: HTMLOptions{ProfilePath: "x", OutputPath: "x"}},
		{name: "missing profile", ctx: context.Background(), opts: HTMLOptions{ProjectDir: "x", OutputPath: "x"}},
		{name: "missing output", ctx: context.Background(), opts: HTMLOptions{ProjectDir: "x", ProfilePath: "x"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := GenerateHTML(test.ctx, test.opts); err == nil {
				t.Fatal("GenerateHTML() error = nil, want validation error")
			}
		})
	}
	projectDir := t.TempDir()
	if err := GenerateHTML(context.Background(), HTMLOptions{
		ProjectDir:  projectDir,
		ProfilePath: "coverage.out",
		OutputPath:  "coverage.out",
	}); err == nil {
		t.Fatal("GenerateHTML() with identical profile and output error = nil, want error")
	}

	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(projectDir, "actual.html")
	writeCoverageProfile(t, filepath.Join(projectDir, "coverage.out"))
	if err := os.WriteFile(target, []byte("actual"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(projectDir, "coverage.html")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := GenerateHTML(context.Background(), HTMLOptions{
		GoCommand:   "/does/not/matter",
		ProjectDir:  projectDir,
		ProfilePath: "coverage.out",
		OutputPath:  "coverage.html",
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("GenerateHTML() error = %v, want symbolic-link rejection", err)
	}
}

func TestGenerateHTMLRejectsSymlinkAncestorsAndProfileAliases(t *testing.T) {
	t.Run("relative output traversal", func(t *testing.T) {
		projectDir := t.TempDir()
		writeCoverageProfile(t, filepath.Join(projectDir, "coverage.out"))
		err := GenerateHTML(context.Background(), HTMLOptions{
			GoCommand:   "/does/not/matter",
			ProjectDir:  projectDir,
			ProfilePath: "coverage.out",
			OutputPath:  filepath.Join("..", "coverage.html"),
		})
		if err == nil || !strings.Contains(err.Error(), "escapes the project directory") {
			t.Fatalf("GenerateHTML() error = %v, want traversal rejection", err)
		}
	})

	t.Run("hard-linked output aliases profile", func(t *testing.T) {
		projectDir := t.TempDir()
		profilePath := filepath.Join(projectDir, "coverage.out")
		outputPath := filepath.Join(projectDir, "coverage.html")
		writeCoverageProfile(t, profilePath)
		if err := os.Link(profilePath, outputPath); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}

		err := GenerateHTML(context.Background(), HTMLOptions{
			GoCommand:   "/does/not/matter",
			ProjectDir:  projectDir,
			ProfilePath: "coverage.out",
			OutputPath:  "coverage.html",
		})
		if err == nil || !strings.Contains(err.Error(), "aliases the coverage profile") {
			t.Fatalf("GenerateHTML() error = %v, want profile-alias rejection", err)
		}
		assertCoverageProfileUnchanged(t, profilePath)
	})

	if runtime.GOOS == "windows" {
		return
	}

	t.Run("output ancestor symlink aliases profile", func(t *testing.T) {
		projectDir := t.TempDir()
		evidenceDir := filepath.Join(projectDir, "evidence")
		if err := os.Mkdir(evidenceDir, 0o700); err != nil {
			t.Fatal(err)
		}
		profilePath := filepath.Join(evidenceDir, "coverage.out")
		writeCoverageProfile(t, profilePath)
		if err := os.Symlink(evidenceDir, filepath.Join(projectDir, "alias")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		err := GenerateHTML(context.Background(), HTMLOptions{
			GoCommand:   "/does/not/matter",
			ProjectDir:  projectDir,
			ProfilePath: filepath.Join("evidence", "coverage.out"),
			OutputPath:  filepath.Join("alias", "coverage.out"),
		})
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("GenerateHTML() error = %v, want symlink-ancestor rejection", err)
		}
		assertCoverageProfileUnchanged(t, profilePath)
	})

	t.Run("profile ancestor symlink", func(t *testing.T) {
		projectDir := t.TempDir()
		actualEvidence := t.TempDir()
		writeCoverageProfile(t, filepath.Join(actualEvidence, "coverage.out"))
		if err := os.Symlink(actualEvidence, filepath.Join(projectDir, "evidence")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		err := GenerateHTML(context.Background(), HTMLOptions{
			GoCommand:   "/does/not/matter",
			ProjectDir:  projectDir,
			ProfilePath: filepath.Join("evidence", "coverage.out"),
			OutputPath:  "coverage.html",
		})
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("GenerateHTML() error = %v, want profile symlink-ancestor rejection", err)
		}
	})

	t.Run("profile target symlink", func(t *testing.T) {
		projectDir := t.TempDir()
		actualProfile := filepath.Join(t.TempDir(), "coverage.out")
		writeCoverageProfile(t, actualProfile)
		if err := os.Symlink(actualProfile, filepath.Join(projectDir, "coverage.out")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		err := GenerateHTML(context.Background(), HTMLOptions{
			GoCommand:   "/does/not/matter",
			ProjectDir:  projectDir,
			ProfilePath: "coverage.out",
			OutputPath:  "coverage.html",
		})
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("GenerateHTML() error = %v, want profile-target symlink rejection", err)
		}
	})

	t.Run("generated file hard-links profile", func(t *testing.T) {
		projectDir := t.TempDir()
		profilePath := filepath.Join(projectDir, "coverage.out")
		writeCoverageProfile(t, profilePath)
		script := writeScript(t, projectDir, "aliasing-go", `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    -o=*) output=${arg#-o=} ;;
  esac
done
rm "$output"
ln "$PWD/coverage.out" "$output"
`)

		err := GenerateHTML(context.Background(), HTMLOptions{
			GoCommand:   script,
			ProjectDir:  projectDir,
			ProfilePath: "coverage.out",
			OutputPath:  "coverage.html",
		})
		if err == nil || !strings.Contains(err.Error(), "aliases coverage profile") {
			t.Fatalf("GenerateHTML() error = %v, want generated-file alias rejection", err)
		}
		assertCoverageProfileUnchanged(t, profilePath)
		if _, statErr := os.Lstat(filepath.Join(projectDir, "coverage.html")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("coverage HTML was published from aliased temporary file, err = %v", statErr)
		}
		assertNoTemporaryFiles(t, projectDir)
	})
}

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCoverageProfile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("mode: set\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertCoverageProfileUnchanged(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "mode: set\n" {
		t.Fatalf("coverage profile changed to %q", data)
	}
}

func restoreGeneratedSnapshotMetadata(
	t *testing.T,
	path string,
	expected generatedFileSnapshot,
) {
	t.Helper()
	if err := os.Chmod(path, expected.mode.Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(
		path,
		expected.modified,
		expected.modified,
	); err != nil {
		t.Fatal(err)
	}
}

func assertGeneratedSnapshotMetadata(
	t *testing.T,
	path string,
	expected generatedFileSnapshot,
	wantSameIdentity bool,
) {
	t.Helper()
	current, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := os.SameFile(expected.identity, current); got != wantSameIdentity {
		t.Fatalf(
			"generated-file identity match = %t, want %t",
			got,
			wantSameIdentity,
		)
	}
	if current.Size() != expected.size ||
		current.Mode() != expected.mode ||
		!current.ModTime().Equal(expected.modified) {
		t.Fatalf(
			"generated-file metadata = (size=%d, mode=%s, mtime=%s), "+
				"want (size=%d, mode=%s, mtime=%s)",
			current.Size(),
			current.Mode(),
			current.ModTime(),
			expected.size,
			expected.mode,
			expected.modified,
		)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("%s mode = %s, want 0600", path, got)
	}
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	var matches []string
	for _, pattern := range []string{
		".coverage.html.tmp-*",
		"..coverage.html.tmp-*.theme-*",
		".coverage.html.theme-*",
	} {
		found, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			t.Fatal(err)
		}
		matches = append(matches, found...)
	}
	if len(matches) > 0 {
		t.Fatalf("coverage HTML temporary files remain: %v", matches)
	}
}

func decorateCoverageHTMLForTest(
	ctx context.Context,
	source io.Reader,
	destination io.Writer,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	anchor := []byte("</head>")
	if bytes.Count(data, anchor) != 1 {
		return errors.New("test coverage HTML has no unique closing head")
	}
	data = bytes.Replace(
		data,
		anchor,
		[]byte(`<style id="test-coverage-theme"></style></head>`),
		1,
	)
	_, err = destination.Write(data)
	return err
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), `'a'\''b'`; got != want {
		t.Fatalf("shellQuote() = %s, want %s", strconv.Quote(got), strconv.Quote(want))
	}
}

func TestBoundedStderr(t *testing.T) {
	var stderr boundedStderr
	stderr.limit = 5
	if n, err := stderr.Write([]byte("123")); err != nil || n != 3 {
		t.Fatalf("first Write() = (%d, %v), want (3, nil)", n, err)
	}
	if n, err := stderr.Write([]byte("456789")); err != nil || n != 6 {
		t.Fatalf("second Write() = (%d, %v), want (6, nil)", n, err)
	}
	got := stderr.String()
	if !strings.Contains(got, "12345") || !strings.Contains(got, "4 bytes omitted") {
		t.Fatalf("bounded stderr = %q, want retained prefix and omission count", got)
	}
}
