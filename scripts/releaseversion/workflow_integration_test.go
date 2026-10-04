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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Execute public Make targets with real Git and only local bare remotes. The
// versioned and CI fixtures permit deterministic failures without installing
// release dependencies or recursively invoking the repository's complete CI.
type releaseFixture struct {
	t                  *testing.T
	root, remote, make string
	env                []string
	initial            string
}

func newReleaseFixture(t *testing.T) *releaseFixture {
	t.Helper()
	for _, tool := range []string{"git", "go", "sh", "make"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("release E2E requires %s: %v", tool, err)
		}
	}
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	f := &releaseFixture{t: t, root: filepath.Join(base, "release checkout"), remote: filepath.Join(base, "origin.git"), make: filepath.Join(source, "Makefile")}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "GITHUB_") || strings.HasPrefix(key, "RELEASE_") || key == "VERSIONED" || key == "MAKEFLAGS" || key == "MFLAGS" || key == "MAKELEVEL" || key == "MAKE" {
			continue
		}
		f.env = append(f.env, value)
	}
	f.env = append(f.env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=Release Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Release Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GOENV=off", "GOFLAGS=", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off",
		"VERSIONED="+filepath.Join(f.root, ".fixture", "versioned"))
	for _, name := range []string{"go.mod", "scripts/release.sh", "scripts/releaseversion/main.go"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		f.write(name, string(data))
	}
	f.write("VERSION", "1.2.3\n")
	f.write("README.md", "fixture\n")
	f.write(".gitignore", ".fixture/\n")
	f.write("Makefile", "release-check:\n\t@sh .fixture/gate\n")
	f.write(".fixture/versioned", `#!/bin/sh
set -eu
mode=${RELEASE_FIXTURE_TOOL_MODE:-}
if [ "$1" = -version ]; then
    [ "$mode" != banner-fail ] || exit 12
    if [ "$mode" = wrong-pin ]; then echo 'versioned 1.0.35, fixture'; else echo 'versioned 1.0.36, fixture'; fi
    exit 0
fi
[ "$1" = -source ] && [ "$4" = -silent ] && [ "$#" -eq 4 ]
[ "$mode" != fail ] || exit 17
[ "$mode" != unchanged ] || exit 0
if [ "$mode" = mismatch ]; then printf '2.0.0\n' > "$2"; exit 0; fi
case "$3" in
    -patch) printf '1.2.4\n' > "$2" ;;
    -minor) printf '1.3.0\n' > "$2" ;;
    *) exit 1 ;;
esac
`)
	if err := os.Chmod(filepath.Join(f.root, ".fixture/versioned"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.write(".fixture/gate", `#!/bin/sh
set -eu
cat VERSION >> .fixture/gates
case "${RELEASE_FIXTURE_GATE_MODE:-}" in
    fail) exit 19 ;;
    tracked) echo changed >> README.md ;;
    untracked) echo changed > unexpected ;;
    staged) echo changed > unexpected; git add unexpected ;;
    commit) git commit --allow-empty -m 'fixture gate advanced HEAD' ;;
    remote-tag) git tag v1.2.4; git push origin refs/tags/v1.2.4; git tag -d v1.2.4 ;;
esac
`)
	f.ok("git", "init", "-q", "-b", "main")
	f.ok("git", "add", ".")
	f.ok("git", "commit", "-qm", "fixture baseline")
	f.initial = strings.TrimSpace(f.ok("git", "rev-parse", "HEAD"))
	f.ok("git", "init", "--bare", "-q", f.remote)
	f.ok("git", "remote", "add", "origin", f.remote)
	f.ok("git", "push", "-q", "-u", "origin", "main")
	return f
}

func (f *releaseFixture) write(name, data string) {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *releaseFixture) command(name string, args ...string) (string, error) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = f.root, f.env
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (f *releaseFixture) ok(name string, args ...string) string {
	f.t.Helper()
	output, err := f.command(name, args...)
	if err != nil {
		f.t.Fatalf("%s %q: %v\n%s", name, args, err, output)
	}
	return output
}

func (f *releaseFixture) release(target string, success bool) string {
	f.t.Helper()
	output, err := f.command("make", "-j2", "-f", f.make, target)
	if (err == nil) != success {
		f.t.Fatalf("make %s: %v\n%s", target, err, output)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".git/tested-release.lock")); !os.IsNotExist(err) {
		f.t.Fatalf("release lock was not cleaned: %v", err)
	}
	return output
}

func (f *releaseFixture) unchanged() {
	f.t.Helper()
	if got := strings.TrimSpace(f.ok("git", "rev-parse", "HEAD")); got != f.initial {
		f.t.Fatalf("unexpected local commit %s", got)
	}
	if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", "main")); got != f.initial {
		f.t.Fatalf("unexpected remote commit %s", got)
	}
	data, err := os.ReadFile(filepath.Join(f.root, "VERSION"))
	if err != nil || string(data) != "1.2.3\n" {
		f.t.Fatalf("VERSION changed: %q, %v", data, err)
	}
}

func (f *releaseFixture) published(target, version string) {
	f.t.Helper()
	tag := "v" + version
	head := strings.TrimSpace(f.ok("git", "rev-parse", "HEAD"))
	if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", "main")); got != head {
		f.t.Fatalf("remote HEAD = %s, want %s", got, head)
	}
	if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "tag")); got != tag {
		f.t.Fatalf("published unexpected tags: %s", got)
	}
	if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "cat-file", "-t", tag)); got != "tag" {
		f.t.Fatalf("expected annotated tag, got %s", got)
	}
	if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", tag+"^{commit}")); got != head {
		f.t.Fatalf("tag points to %s, want %s", got, head)
	}
	if got := strings.TrimSpace(f.ok("git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")); got != "VERSION" {
		f.t.Fatalf("release changed %s", got)
	}
	if got := f.ok("git", "status", "--porcelain"); got != "" {
		f.t.Fatalf("release left dirty tree: %s", got)
	}
	if got := f.ok("go", "run", "./scripts/releaseversion", "check", tag); strings.TrimSpace(got) != version {
		f.t.Fatalf("VERSION = %s", got)
	}
	message := f.ok("git", "show", "-s", "--format=%B", "HEAD")
	for _, text := range []string{"ops: release " + tag, "Before this commit:", "After this commit:", "Tests:", "More info: make " + target} {
		if !strings.Contains(message, text) {
			f.t.Fatalf("missing %q in commit: %s", text, message)
		}
	}
	if strings.HasPrefix(target, "fast-") {
		if !strings.Contains(message, "skipped explicitly") || strings.Contains(message, "passed before") {
			f.t.Fatalf("fast release claimed local validation: %s", message)
		}
		if _, err := os.Stat(filepath.Join(f.root, ".fixture/gates")); !os.IsNotExist(err) {
			f.t.Fatalf("fast release ran gate: %v", err)
		}
	} else {
		gates, err := os.ReadFile(filepath.Join(f.root, ".fixture/gates"))
		if err != nil || string(gates) != "1.2.3\n" {
			f.t.Fatalf("expected exactly one gate: %q, %v", gates, err)
		}
	}
}

func TestReleaseWorkflowTargets(t *testing.T) {
	for _, target := range []string{"release", "minor-release", "fast-release", "fast-minor-release"} {
		t.Run(target, func(t *testing.T) {
			f := newReleaseFixture(t)
			// Optional qualification against an explicitly selected installed pin.
			// The normal E2E suite needs no release dependency or network download.
			if tool := os.Getenv("TESTED_RELEASE_VERSIONED"); tool != "" {
				f.env = append(f.env, "VERSIONED="+tool)
			}
			f.ok("git", "tag", "-a", "unrelated-local-tag", "-m", "unrelated")
			f.ok("git", "config", "push.followTags", "true")
			if strings.HasPrefix(target, "fast-") {
				f.env = append(f.env, "RELEASE_FIXTURE_GATE_MODE=fail")
			}
			f.release(target, true)
			version := "1.2.4"
			if strings.Contains(target, "minor") {
				version = "1.3.0"
			}
			f.published(target, version)
		})
	}
}

func TestReleaseWorkflowPreflightFailures(t *testing.T) {
	for _, mode := range []string{"tracked", "staged", "untracked", "branch", "detached", "local-tag", "remote-tag", "no-remote", "unreachable", "multiple-push-urls", "wrong-pin", "banner-fail", "fail", "mismatch", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			f := newReleaseFixture(t)
			switch mode {
			case "tracked":
				f.write("README.md", "changed")
			case "staged", "untracked":
				f.write("unexpected", "change")
				if mode == "staged" {
					f.ok("git", "add", "unexpected")
				}
			case "branch":
				f.ok("git", "checkout", "-qb", "feature")
			case "detached":
				f.ok("git", "checkout", "--detach", "-q")
			case "local-tag", "remote-tag":
				f.ok("git", "tag", "v1.2.4")
				if mode == "remote-tag" {
					f.ok("git", "push", "origin", "refs/tags/v1.2.4")
					f.ok("git", "tag", "-d", "v1.2.4")
				}
			case "no-remote":
				f.ok("git", "remote", "remove", "origin")
			case "unreachable":
				f.ok("git", "remote", "set-url", "origin", filepath.Join(f.root, "missing.git"))
			case "multiple-push-urls":
				f.ok("git", "remote", "set-url", "--add", "--push", "origin", f.remote)
				f.ok("git", "remote", "set-url", "--add", "--push", "origin", filepath.Join(f.root, "second.git"))
			default:
				f.env = append(f.env, "RELEASE_FIXTURE_TOOL_MODE="+mode)
			}
			for _, target := range []string{"release", "fast-release"} {
				f.release(target, false)
				f.unchanged()
			}
			if _, err := os.Stat(filepath.Join(f.root, ".fixture/gates")); !os.IsNotExist(err) {
				t.Fatalf("invalid preflight ran tests: %v", err)
			}
		})
	}
}

func TestReleaseWorkflowGateFailures(t *testing.T) {
	for _, mode := range []string{"fail", "tracked", "staged", "untracked", "commit", "remote-tag"} {
		t.Run(mode, func(t *testing.T) {
			f := newReleaseFixture(t)
			f.env = append(f.env, "RELEASE_FIXTURE_GATE_MODE="+mode)
			f.release("release", false)
			if mode == "commit" {
				// The fixture itself advanced HEAD, but no release commit/tag followed.
				if got := strings.TrimSpace(f.ok("git", "show", "-s", "--format=%s")); got != "fixture gate advanced HEAD" {
					t.Fatalf("unexpected commit %s", got)
				}
			} else {
				f.unchanged()
			}
			if got := f.ok("git", "tag"); got != "" {
				t.Fatalf("gate failure created tags: %s", got)
			}
		})
	}
}

func TestReleaseWorkflowStaleBranch(t *testing.T) {
	for _, diverged := range []bool{false, true} {
		t.Run(map[bool]string{false: "behind", true: "diverged"}[diverged], func(t *testing.T) {
			f := newReleaseFixture(t)
			f.ok("git", "commit", "--allow-empty", "-qm", "advance remote")
			f.ok("git", "push", "origin", "main")
			remoteHead := strings.TrimSpace(f.ok("git", "rev-parse", "HEAD"))
			f.ok("git", "reset", "--hard", f.initial)
			if diverged {
				f.ok("git", "commit", "--allow-empty", "-qm", "diverge locally")
			}
			head := f.ok("git", "rev-parse", "HEAD")
			for _, target := range []string{"minor-release", "fast-minor-release"} {
				f.release(target, false)
			}
			if got := f.ok("git", "rev-parse", "HEAD"); got != head {
				t.Fatalf("failed release changed HEAD: %s", got)
			}
			if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", "main")); got != remoteHead {
				t.Fatalf("failed release changed remote: %s", got)
			}
			if got := f.ok("git", "status", "--porcelain"); got != "" {
				t.Fatalf("failed release modified tree: %s", got)
			}
		})
	}
}

func TestReleaseWorkflowAtomicPush(t *testing.T) {
	for _, target := range []string{"minor-release", "fast-minor-release"} {
		t.Run(target, func(t *testing.T) {
			f := newReleaseFixture(t)
			hook := filepath.Join(f.remote, "hooks/update")
			if err := os.WriteFile(hook, []byte("#!/bin/sh\ncase \"$1\" in refs/tags/*) exit 1 ;; esac\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			f.release(target, false)
			if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", "main")); got != f.initial {
				t.Fatalf("atomic push advanced branch without tag: %s", got)
			}
			if got := f.ok("git", "--git-dir", f.remote, "tag"); got != "" {
				t.Fatalf("rejected tag published: %s", got)
			}
			if got := strings.TrimSpace(f.ok("git", "tag")); got != "v1.3.0" {
				t.Fatalf("local recovery tag missing: %s", got)
			}
			// A retry must stop on the existing local release, not increment again.
			head := f.ok("git", "rev-parse", "HEAD")
			f.release(target, false)
			if got := f.ok("git", "rev-parse", "HEAD"); got != head {
				t.Fatalf("retry created another release: %s", got)
			}
		})
	}
}

func TestReleaseWorkflowCheckAndPartialTargets(t *testing.T) {
	f := newReleaseFixture(t)
	f.release("release-git-check", true)
	f.unchanged()
	if got := f.ok("git", "status", "--porcelain"); got != "" {
		t.Fatalf("check modified worktree: %s", got)
	}
	for _, target := range []string{"release-update-version", "release-git-commit"} {
		f.release(target, false)
		f.unchanged()
	}
	for _, args := range [][]string{{"major"}, {"patch", "--skip-test"}, {"minor", "--skip-tests", "extra"}, {"check", "--skip-tests"}} {
		if output, err := f.command("sh", append([]string{"scripts/release.sh"}, args...)...); err == nil {
			t.Fatalf("accepted invalid release arguments %q: %s", args, output)
		}
		f.unchanged()
	}
}

func TestReleaseWorkflowVersionPolicy(t *testing.T) {
	for _, value := range []string{"0.2.3\n", "2.0.0\n", "1.02.3\n", "1.2.3-dev\n", "1.2.3\n\n", "1.2.3\r\n", "1.0.18446744073709551615\n"} {
		t.Run(strings.TrimSpace(value), func(t *testing.T) {
			f := newReleaseFixture(t)
			f.write("VERSION", value)
			f.ok("git", "add", "VERSION")
			f.ok("git", "commit", "-qm", "fixture invalid release version")
			head := f.ok("git", "rev-parse", "HEAD")
			for _, target := range []string{"release", "fast-release"} {
				f.release(target, false)
			}
			if got := f.ok("git", "rev-parse", "HEAD"); got != head {
				t.Fatalf("invalid version created a commit: %s", got)
			}
			if got := f.ok("git", "status", "--porcelain"); got != "" {
				t.Fatalf("invalid version modified the worktree: %s", got)
			}
			if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", "main")); got != f.initial {
				t.Fatalf("invalid version published a commit: %s", got)
			}
		})
	}
}

func TestReleaseWorkflowPushDestination(t *testing.T) {
	f := newReleaseFixture(t)
	fetchRemote := f.remote
	f.remote = filepath.Join(filepath.Dir(f.remote), "push.git")
	f.ok("git", "init", "--bare", "-q", f.remote)
	f.ok("git", "push", f.remote, "main")
	f.ok("git", "remote", "set-url", "--push", "origin", f.remote)
	// An unrelated tag at the fetch URL must not affect the push destination.
	f.ok("git", "--git-dir", fetchRemote, "tag", "v1.2.4", "main")
	f.release("fast-release", true)
	f.published("fast-release", "1.2.4")
	if got := strings.TrimSpace(f.ok("git", "--git-dir", fetchRemote, "rev-parse", "main")); got != f.initial {
		t.Fatalf("release modified the fetch-only repository: %s", got)
	}
}

func TestReleaseWorkflowCurrentReleaseRecovery(t *testing.T) {
	for _, mode := range []string{"missing-tag", "unpublished-tag", "published-tag"} {
		t.Run(mode, func(t *testing.T) {
			f := newReleaseFixture(t)
			f.ok("git", "commit", "--allow-empty", "-qm", "ops: release v1.2.3")
			f.initial = strings.TrimSpace(f.ok("git", "rev-parse", "HEAD"))
			f.ok("git", "push", "origin", "main")
			if mode != "missing-tag" {
				f.ok("git", "tag", "-a", "v1.2.3", "-m", "v1.2.3")
			}
			if mode == "published-tag" {
				f.ok("git", "push", "origin", "refs/tags/v1.2.3")
				f.release("release-git-check", true)
			} else {
				output := f.release("fast-release", false)
				if !strings.Contains(output, "recover it before releasing again") {
					t.Fatalf("missing recovery diagnosis: %s", output)
				}
			}
			f.unchanged()
		})
	}
}

func TestReleaseWorkflowCommitFailure(t *testing.T) {
	f := newReleaseFixture(t)
	f.write(".git/hooks/pre-commit", "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(f.root, ".git/hooks/pre-commit"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.release("fast-release", false)
	if got := strings.TrimSpace(f.ok("git", "rev-parse", "HEAD")); got != f.initial {
		t.Fatalf("failed commit advanced HEAD: %s", got)
	}
	if got := strings.TrimSpace(f.ok("git", "--git-dir", f.remote, "rev-parse", "main")); got != f.initial {
		t.Fatalf("failed commit published: %s", got)
	}
	if got := f.ok("git", "tag"); got != "" {
		t.Fatalf("failed commit created tag: %s", got)
	}
	f.release("fast-release", false)
}

func TestReleaseWorkflowBuildRejectsOtherMajors(t *testing.T) {
	f := newReleaseFixture(t)
	f.write("VERSION", "2.0.0\n")
	for _, target := range []string{"version-check", "info", "build", "install"} {
		output := f.release(target, false)
		if !strings.Contains(output, "VERSION must be exactly 1.<minor>.<patch>") {
			t.Fatalf("%s did not enforce the version policy: %s", target, output)
		}
	}
}

func TestReleaseWorkflowLock(t *testing.T) {
	f := newReleaseFixture(t)
	lock := filepath.Join(f.root, ".git/tested-release.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := f.command("make", "-f", f.make, "fast-release")
	if err == nil || !strings.Contains(output, "another release is running") {
		t.Fatalf("concurrent release was not refused: %v, %s", err, output)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("release removed another process's lock: %v", err)
	}
	f.unchanged()
}
