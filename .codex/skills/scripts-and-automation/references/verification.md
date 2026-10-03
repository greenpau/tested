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
Check exact offline rerendering, DOM identity, filtering, keyboard controls,
print restoration, narrow/light/dark layouts, security, and the no-script
fallback. Keep screenshots, PDFs, and failure traces in the ignored
`scripts/browser/test-results/` directory. These checks qualify Chromium and
WebKit test reports, not coverage-explorer interactions or other browser engines.

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
