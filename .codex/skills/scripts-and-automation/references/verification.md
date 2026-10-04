# Development, Fixtures, and CI

## Select direct development commands

Use the module's declared Go toolchain and prefer narrow commands while editing:

```text
gofmt -w <changed-go-files>
go test ./pkg/<owner>
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Preserve the normal inherited environment when qualifying the suite. Some
POSIX permission fixtures currently assume an ordinary `022` creation mask;
changing the command's umask to `077` makes those fixtures fail before their
intended assertion. Secure external verification logs by pre-creating the log
files with `0600`, rather than changing the child tests' creation mask. Inspect
[permission fixtures](../../../../pkg/artifact/layout_test.go) before claiming
qualification under a different umask.

Use a focused `-run` expression for one test and a time-bounded test context
when exercising subprocess cancellation. Do not use `go test` pipelines that
lose the left-hand exit status.

Keep the Make bootstrap stages observable independently of `tested` itself.
Use `-v -p 1` for direct unit, race, and shuffled checks so test/subtest names,
logs, and outcomes stream within each package instead of waiting for package
completion. Use `-count=1` for unit and race checks to avoid cached replay;
retain `-shuffle=on -count=3` for shuffled checks. Preserve the selected
`TEST_DIR` patterns and Go's within-package parallel-test behavior. Validate
changes to these flags through the actual Make targets and their live output.

Keep local build identity collection shared by `make info`, `make build`, and
`make install` in [the build script](../../../../scripts/build.sh). Stamp a full
commit hash, mark tracked or untracked changes dirty, identify detached HEAD,
and collect the actual UTC build time and builder. Preserve argument boundaries
through Go's linker-flag parsing; branch text must never execute as shell code.
Let `go install` choose the destination from GOBIN/GOPATH and print that path.
Ordinary builds must not implicitly install or publish.
Keep `info`, `build`, `install`, `ci`, and release preflight using the shared
`version-check` policy: major 1, canonical decimal components, and no suffixes.
Use the same validator in tagged CI, including exact tag/version equality;
avoid separate permissive shell validators that can drift from release rules.

Use `make e2e-version` (also included by `make e2e`) to qualify the actual CLI's
module metadata fallback, VCS-disabled builds, clean/dirty/detached builds,
quoted branch names, GOBIN/GOPATH paths containing spaces, and release linker
templates. The [integration test](../../../../version_integration_test.go)
uses a temporary file module proxy and private module cache, with networking
disabled. Pass `-modcacherw` for this disposable cache so test cleanup can remove
downloaded module directories. These checks exercise built binaries and release
stamps; they do not replace GoReleaser archive/package preflight or qualify
installation on an untested native host.

Use `make e2e-release` (included by `make e2e`) for release automation changes.
The [release tests](../../../../scripts/releaseversion/workflow_integration_test.go)
invoke the actual public Make targets with real Git and local bare remotes.
Keep version-tool and quality-gate failure injection inside disposable fixtures;
ordinary CI must not install a publishing dependency or recursively run itself.
Optionally select an already installed `versioned` 1.0.36 using
`TESTED_RELEASE_VERSIONED=/path/to/versioned` when running
`go test -v -count=1 -tags=integration ./scripts/releaseversion -run '^TestReleaseWorkflowTargets$'`.
Distinguish controlled-tool workflow evidence from actual pinned-tool results.

Cover all four targets, exact version-only commits and annotated tags, checked
gate execution once versus fast gate omission, truthful commit test status,
unrelated tags under `push.followTags`, and separate fetch/push destinations.
Require dirty/staged/untracked work, wrong/detached branches, invalid major or
version syntax, overflow, missing/unreachable/multiple remotes, existing tags,
stale/diverged branches, failed or unexpected tool output, failed/dirty gates,
and changed HEAD to stop later mutation. Test atomic rejection with a real
remote hook, retry recovery, commit-hook failure, lock ownership, and refusal
of partial targets. Pair these with unit tests for canonical version parsing,
uint64 bounds, patch/minor increments, and exact tag matching. These checks do
not publish remotely or replace tagged CI's archive/native-host preflight.

Cross-build the CLI for each supported target without running the foreign
binary. At minimum, cover Linux, Darwin, and Windows on both amd64 and arm64;
retain any documented compatibility target such as Windows 386. Keep
platform-specific runner files behind build constraints and make a failed
target explicit instead of silently reducing the matrix.

Use a built `tested` binary against controlled modules under `testdata` for
end-to-end verification. Cover a passing fixture, a normal failing test, a
build failure, and coverage/report generation. Keep cancellation and
process-tree assertions in time-bounded Go integration tests on each native
host, including app-level manifest withholding, instead of adding
timing-sensitive shell signal choreography. Store disposable outputs below
`t.TempDir`, `tmp`, or an ignored managed directory.

Exercise repeated CPU benchmark occurrences and a benchmark that explicitly
suppresses `ns/op` with `ReportMetric(0, "ns/op")`. Assert their normalized
occurrence counts and absence of incompleteness; do not depend on whether
`test2json` nondeterministically coalesces or splits the benchmark-name prefix
and result writes.

Keep the fixture module buildable with the minimum Go release. Exercise a
Go 1.25 `testing.T.Attr` test directly and place any `testing.T.ArtifactDir`
fixture in a `go1.26`-constrained file. On Go 1.26, run the artifact fixture
with `-artifacts` and a pre-created `-outputdir`; verify raw `attr`/`artifacts`
records and redacted JSON, HTML, and JUnit projections, and reject clickable
child-provided artifact paths. Gate those flags and Go 1.26 BuildEvent
assertions using the active `go env GOVERSION`; on Go 1.25, run the remaining
end-to-end contract and print an explicit versioned skip.

After a coherent fixture run, invoke `go run ./scripts/bundlecheck` to verify
the exact conditional inventory, private modes on POSIX hosts, manifest order,
security metadata, sizes, and SHA-256 bindings independently from the product
package. Copy deterministic derivative baselines outside `.coverage`, run
offline `tested report`, compare exact bytes, and validate the rebuilt bundle
again.

## Qualify interactive test reports in browsers

Use `make e2e-browser` for test-report layout and interaction changes. The
target builds the current binary, then runs the pinned Playwright development
suite in [scripts/browser](../../../../scripts/browser/). Install its tooling
explicitly with `npm --prefix scripts/browser ci` and
`npm --prefix scripts/browser exec -- playwright install chromium webkit`;
on Linux, add `--with-deps` when browser system libraries are needed. Require
Node.js 20 or newer. Keep these dependencies out of the Go runtime and ordinary
Go-only Make targets; `make ci` does not install or run browser tooling.

Open generated reports through `file://`, without a web server. Use the
controlled [browser fixture](../../../../testdata/browser/) for repeated,
parallel, deep, skipped, failing, and redacted subtests; use imported evidence
for missing parents, incomplete runs, build-only cases, and empty streams.
Assert the expected nonzero CLI results instead of masking them. Resolve the
test-owned temporary root through its real path on macOS before giving it to
the managed artifact writer, and remove only that owned directory on teardown.
Check exact offline rerendering, DOM identity, independent package/test/output
filters, their intersection with status, clear-all state restoration, compact
row heights, expandable output/metadata, keyboard controls, print restoration,
narrow/light/dark layouts, security, and the no-script fallback. Exercise both
summary tables' default package ordering, natural text ordering, exact duration
and statement sorting (including integers beyond JavaScript's safe-number
range), and coverage ratios that display the same rounded percentage. Check
both directions, unavailable values, stable ties, accessible sort state,
visible package labels in every row after every sort, independent table state,
and contained scrolling.
Use a CLI fixture with more timed occurrences than `--slowest` permits to
verify that package and duration sorting retain the selected longest attempts,
including repeated test names. Pair it with unit cases for disabled, single,
and larger limits and preservation of source ordering.
Qualify the continuous package list separately: duration adjacent to coverage
at the right of each desktop header, with status alongside,
per-package and global disclosure (including mixed state and filtered-out
packages), saved output/branch state, all package sort keys and directions,
missing-versus-zero metrics, DOM identity, narrow layouts, and print/no-script
fallbacks. Use unequal per-file statement weights in a real imported profile;
pair browser checks with unit cases for exact package matching, redaction
collisions, subpackage exclusion, unavailable evidence, and overflow rejection.
Exercise module-relative labels from a subdirectory and a subpackage-only run,
root `.`, external and similarly prefixed packages, full-name tooltips/filtering,
and Base package in Run assessment. Check that no-script and print retain all
package labels. Pair browser checks with module-boundary and bounded-file tests,
whole-path redaction cases, and unchanged JSON/JUnit identities.
Verify that whole-path redaction can remove a directory without turning a
coverage row into an apparent module-root file, including after table sorting.
Exercise a dependency literally named `module` inside a block before the actual
module directive. Cover optional block whitespace, duplicate directives,
unclosed blocks, exact/over-limit file sizes, and symlink boundaries in unit
tests. Check failed and incomplete package output/metadata starting open in both
browsers, then manually close details and verify package toggles retain that
choice.
Use a disposable local Git module to compare both run and offline report modes
against an explicit baseline. Cover unchanged/modified/rename-only/untracked
source packages, test-file-only edits, missing baselines and comparison failure.
Verify shared membership between the test switch and coverage file selector,
combined filters, reset and saved disclosure state, zero/absent comparison,
print/no-script fallback, and navigation to the actual local index from both
pages (including unsupported coverage explorer markup). Activate custom
switches through their visible labels for pointer tests and focus/Space for
keyboard tests; the underlying checkbox is visually clipped.
Keep screenshots, PDFs, and failure traces in the ignored
`scripts/browser/test-results/` directory. These checks qualify Chromium and
WebKit test reports plus the covered navigation/change-selection interactions;
they do not qualify the entire coverage explorer or other browser engines.

## Keep CI grounded in the checked-in workflows

Inspect [CI](../../../../.github/workflows/ci.yml) before changing its matrix.
The current jobs exercise Go 1.25 minimum compatibility, Go 1.26 conformance,
and native Windows and Darwin process behavior. Keep direct checks authoritative
before trusting the tool's self-test. Preserve this ordering where applicable:

1. check out the repository and install the job's selected Go version while
   retaining a job for the minimum declared by `go.mod`;
2. verify the toolchain with `GOTOOLCHAIN=local`; the current compatibility and
   conformance jobs disable setup-go caching. If caches are added, bound them
   and exclude report evidence;
3. verify formatting, then run `go test ./...` and `go vet ./...`;
4. run `go test -race ./...` and shuffled repetitions on a supported host;
5. build the current CLI, use it to test its own module once, and independently
   validate the resulting root `.coverage` inventory and manifest;
6. run the built CLI against passing, failing, and build-failing fixtures;
7. cross-build the CLI for the supported matrix;
8. verify exit precedence, file modes where the platform supports them,
   manifest checksums, exact artifact names, and deterministic rerendering;
9. upload `.coverage` only as a diagnostic artifact, including on fixture
   failure, with access and retention appropriate for potentially sensitive raw
   evidence.

In the Go 1.26 conformance job, name the root bundle
`tested-go-1.26-coverage-YYYYMMDD-HHMMSS`, with `tested` as the literal
repository prefix and the suffix as the UTC upload timestamp. Produce the name
in an `always()` Bash step immediately before the final `always()` upload,
write it to `$GITHUB_OUTPUT`, and consume that step output as the artifact
name. Upload the complete hidden `.coverage/` directory instead of enumerating
its current files so future managed derivatives remain available as diagnostic
evidence.

The Go 1.26 conformance job also installs Node.js 24 through a pinned setup-node
action, runs `npm ci` against the browser lockfile, explicitly installs Chromium
and WebKit with their system dependencies, and runs `make e2e-browser`. Preserve
the explicit installation steps instead of hiding downloads inside the Make
target. The Go 1.25 minimum and native lifecycle jobs remain independent of
browser dependencies.

Add operating systems to the matrix when they exercise distinct process-tree
behavior. Do not mark Windows, Darwin, or Linux cancellation supported solely
because it cross-compiles; run platform-specific lifecycle tests on each
native host. At minimum, run the Windows runner suite on a native Windows CI
worker when the product claims Job Object ownership and the Darwin suite on a
native macOS worker, while retaining cross-compilation for every documented
architecture.

Pin third-party actions and release tooling to reviewed versions. The product
replaces `tparse` and `go-test-report`; repository validation must not require
installing either tool unless an explicit compatibility comparison test needs
a pinned reference version.

## Manage generated artifacts

Treat these as generated and ignored unless the user explicitly asks to retain
them:

```text
bin/
dist/
.doc/
.coverage/
tmp/
*.test
*.out
*.coverprofile
```

Review `git status --short` after automation. Do not keep changes to `go.mod`,
`go.sum`, `VERSION`, license headers, source, or documentation as incidental
test output.

Run cleanup only when requested or when a test-owned temporary directory can be
removed safely. Resolve every cleanup target to a specific repository child;
never use an empty variable, home directory, workspace root, or unvalidated
glob as a recursive target.

## Preserve automation boundaries

Keep Make targets thin, composable, and non-interactive. Separate formatting,
unit/race/shuffle checks, cross-compilation, fixtures, and publication. Do not
hide dependency installation, `go mod tidy`, license rewrites, version changes,
staging, commits, tags, or pushes inside ordinary test/build/CI targets.
Preserve underlying exit status and identify the failing stage.

Keep direct formatting, vet, unit, race, and shuffled checks authoritative
until they pass. Then build the checked-out source for one final `-count=1`
self-test. Give it sole ownership of root `.coverage`, require coverage, and
validate the bundle only after a zero child exit. Remove that explicit
generated root before the attempt so stale files cannot appear current.
Keep `coverage` reusing `test` instead of adding another root writer.

The current `self-test` also explicitly requests `--coverage-diff-base HEAD`.
Treat that as a choice made by this automation, never as a product default.
The artifact validator lives in [bundlecheck](../../../../scripts/bundlecheck/);
fixture sources live in [testdata](../../../../testdata/). Neither the skill
validator nor a cross-build qualifies a fixture's runtime result.
