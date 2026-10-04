//go:build integration

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

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"
	"time"
)

// These tests build and execute the real CLI, including a network-free module
// installation. Keep them in the explicit E2E target, outside the self-test.
func TestVersionBuild(t *testing.T) {
	for _, tool := range []string{"go", "git", "make", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("E2E requires %s: %v", tool, err)
		}
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(temp, "source")
	files := copyVersionSource(t, root, source)
	binDir := filepath.Join(temp, "installed binaries")
	executable := "tested"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	env := append(os.Environ(), "GOENV=off", "GOFLAGS=", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GONOSUMDB=*", "GONOPROXY=", "GOPRIVATE=", "GOBIN="+binDir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_DATE=2001-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2001-02-03T04:05:06Z")
	command := func(t *testing.T, dir string, extra []string, name string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Env = append(append([]string{}, env...), extra...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %q: %v\n%s", name, args, err, output)
		}
		return string(output)
	}
	t.Run("module installation", func(t *testing.T) {
		proxy := filepath.Join(temp, "proxy")
		versionProxy(t, proxy, source, files)
		proxyPath := filepath.ToSlash(proxy)
		if filepath.VolumeName(proxy) != "" {
			proxyPath = "/" + proxyPath
		}
		proxyURL := (&url.URL{Scheme: "file", Path: proxyPath}).String()
		command(t, temp, []string{"GOPROXY=" + proxyURL, "GOMODCACHE=" + filepath.Join(temp, "modules"), "GOFLAGS=-modcacherw"}, "go", "install", "github.com/greenpau/tested@v1.2.3")
		output := command(t, temp, nil, filepath.Join(binDir, executable), "version")
		requireVersionLines(t, output, "tested 1.2.3", "  commit: not recorded", "  branch: not recorded", "  built: not recorded by not recorded")
	})
	command(t, source, nil, "git", "init", "-b", "main")
	command(t, source, nil, "git", "add", ".")
	command(t, source, nil, "git", "-c", "user.name=Version Test", "-c", "user.email=version@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	revision := strings.TrimSpace(command(t, source, nil, "git", "rev-parse", "HEAD"))
	t.Run("ordinary Go build", func(t *testing.T) {
		binary := filepath.Join(temp, "plain-"+executable)
		command(t, source, nil, "go", "build", "-o", binary, ".")
		output := command(t, temp, nil, binary, "version")
		requireVersionLines(t, output, "  commit: "+revision, "  branch: not recorded", "  built: not recorded by not recorded", "  commit time: 2001-02-03T04:05:06Z")
	})
	t.Run("VCS disabled", func(t *testing.T) {
		binary := filepath.Join(temp, "no-vcs-"+executable)
		command(t, source, nil, "go", "build", "-buildvcs=false", "-o", binary, ".")
		output := command(t, temp, nil, binary, "version")
		requireVersionLines(t, output, "tested dev", "  commit: not recorded", "  branch: not recorded")
	})
	version := strings.TrimSpace(string(readVersionFile(t, filepath.Join(source, "VERSION"))))
	checkStamped := func(t *testing.T, output, branch, commit string, since time.Time) {
		t.Helper()
		requireVersionLines(t, output, "tested "+version, "  commit: "+commit, "  branch: "+branch)
		if strings.Contains(output, "not recorded") || strings.Contains(output, "unknown") || strings.Contains(output, "note:") {
			t.Fatalf("incomplete stamped version: %s", output)
		}
		for _, line := range strings.Split(output, "\n") {
			if !strings.HasPrefix(line, "  built: ") {
				continue
			}
			date, user, ok := strings.Cut(strings.TrimPrefix(line, "  built: "), " by ")
			built, err := time.Parse(time.RFC3339, date)
			if !ok || user == "" || err != nil || built.Before(since.Truncate(time.Second)) || built.After(time.Now()) {
				t.Fatalf("invalid build provenance: %s", line)
			}
			return
		}
		t.Fatal("missing build timestamp")
	}
	t.Run("stamped clean build", func(t *testing.T) {
		since := time.Now()
		output := command(t, source, nil, "make", "build")
		checkStamped(t, output, "main", revision, since)
	})
	t.Run("dirty installation", func(t *testing.T) {
		original := readVersionFile(t, filepath.Join(source, "main.go"))
		writeVersionFile(t, filepath.Join(source, "main.go"), append(original, []byte("\n// Uncommitted edit.\n")...))
		since := time.Now()
		command(t, source, nil, "make", "install")
		output := command(t, temp, nil, filepath.Join(binDir, executable), "version")
		checkStamped(t, output, "main", revision+"-dirty", since)
		writeVersionFile(t, filepath.Join(source, "main.go"), original)
	})
	t.Run("untracked files mark build dirty", func(t *testing.T) {
		file := filepath.Join(source, "untracked.txt")
		writeVersionFile(t, file, []byte("untracked"))
		since := time.Now()
		output := command(t, source, nil, "make", "build")
		checkStamped(t, output, "main", revision+"-dirty", since)
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("detached checkout", func(t *testing.T) {
		command(t, source, nil, "git", "checkout", "--detach", "HEAD")
		since := time.Now()
		output := command(t, source, nil, "make", "build")
		checkStamped(t, output, "detached", revision, since)
	})
	t.Run("branch quoting", func(t *testing.T) {
		branch := "feature/quotes'\"$(id)"
		command(t, source, nil, "git", "checkout", "-b", branch)
		since := time.Now()
		output := command(t, source, nil, "make", "build")
		checkStamped(t, output, branch, revision, since)
	})
	t.Run("GOPATH installation", func(t *testing.T) {
		gopath := filepath.Join(temp, "go path")
		since := time.Now()
		command(t, source, []string{"GOBIN=", "GOPATH=" + gopath}, "make", "install")
		output := command(t, temp, nil, filepath.Join(gopath, "bin", executable), "version")
		checkStamped(t, output, "feature/quotes'\"$(id)", revision, since)
	})
	t.Run("release linker stamps", func(t *testing.T) {
		// Execute the linker templates from the actual release configuration.
		// Archive/package publication is deliberately outside this E2E test.
		config := string(readVersionFile(t, filepath.Join(root, ".goreleaser.yaml")))
		var flags []string
		for _, line := range strings.Split(config, "\n") {
			if flag, ok := strings.CutPrefix(line, "      - -X main."); ok {
				flags = append(flags, "-X main."+flag)
			}
		}
		if len(flags) != 5 {
			t.Fatalf("expected five release identity stamps, got %v", flags)
		}
		for _, branch := range []string{"", "HEAD", "main"} {
			t.Run("branch="+branch, func(t *testing.T) {
				since := time.Now()
				tmpl, err := template.New("release").Option("missingkey=error").Parse(strings.Join(flags, " "))
				if err != nil {
					t.Fatal(err)
				}
				var rendered bytes.Buffer
				if err := tmpl.Execute(&rendered, map[string]any{
					"Version": version, "Branch": branch, "FullCommit": revision,
					"IsGitDirty": true, "Date": since.UTC().Format(time.RFC3339),
					"CommitDate": "2001-02-03T04:05:06Z",
				}); err != nil {
					t.Fatal(err)
				}
				binary := filepath.Join(temp, "release-"+executable)
				command(t, source, nil, "go", "build", "-ldflags="+rendered.String(), "-o", binary, ".")
				output := command(t, temp, nil, binary, "version")
				wantBranch := branch
				if branch == "" || branch == "HEAD" {
					wantBranch = "detached"
				}
				checkStamped(t, output, wantBranch, revision+"-dirty", since)
				if !strings.Contains(output, " by goreleaser\n") {
					t.Fatalf("missing release builder: %s", output)
				}
			})
		}
	})
}

func copyVersionSource(t *testing.T, root, target string) []string {
	t.Helper()
	files := []string{"main.go", "go.mod", "Makefile", "VERSION", ".gitignore", "scripts/build.sh"}
	for _, dir := range []string{"pkg", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if (strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")) || strings.Contains(filepath.ToSlash(rel), "/assets/") {
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		writeVersionFile(t, filepath.Join(target, file), readVersionFile(t, filepath.Join(root, file)))
	}
	return files
}

func versionProxy(t *testing.T, proxy, source string, files []string) {
	t.Helper()
	const module = "github.com/greenpau/tested"
	const version = "v1.2.3"
	base := filepath.Join(proxy, filepath.FromSlash(module), "@v", version)
	writeVersionFile(t, filepath.Join(filepath.Dir(base), "list"), []byte(version+"\n"))
	writeVersionFile(t, base+".mod", readVersionFile(t, filepath.Join(source, "go.mod")))
	writeVersionFile(t, base+".info", []byte(`{"Version":"v1.2.3","Time":"2001-02-03T04:05:06Z"}`))
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, file := range files {
		writer, err := archive.Create(module + "@" + version + "/" + filepath.ToSlash(file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(readVersionFile(t, filepath.Join(source, file))); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	writeVersionFile(t, base+".zip", buf.Bytes())
}

func requireVersionLines(t *testing.T, output string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains("\n"+output, "\n"+line+"\n") {
			t.Errorf("missing version line %q in:\n%s", line, output)
		}
	}
}

func readVersionFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeVersionFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(fmt.Errorf("write fixture: %w", err))
	}
}
