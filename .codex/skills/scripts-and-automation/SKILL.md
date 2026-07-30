---
name: scripts-and-automation
description: Design, add, select, run, review, or document tested repository automation for formatting, Go tests, race checks, cross-builds, fixture reports, CI, packaging, versions, and releases. Use when working with Makefiles, scripts, GitHub Actions, generated build artifacts, dependency tooling, cleanup, or release workflows for tested.
---

# Scripts and Automation

Inspect the repository before naming or running an automation target. The
commands and target families below define the intended automation contract;
they do not assert that a `Makefile`, workflow, release configuration, or named
target already exists. When automation is absent, use the direct Go command or
add the smallest requested wrapper.

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

Use a focused `-run` expression for one test and a time-bounded test context
when exercising subprocess cancellation. Do not use `go test` pipelines that
lose the left-hand exit status.

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

## Shape the intended Make surface

When adding or revising a Makefile, keep targets thin, composable, and
non-interactive. Prefer these responsibilities:

- `fmt` or `fmt-check`: format files or verify that `gofmt` makes no diff;
- `test`: run direct bootstrap checks, then finish with one canonical
  self-hosted all-package run that leaves a validated root `.coverage`;
- `self-test`: build the current source, remove only the generated root
  `.coverage`, run that binary once with coverage, and validate the exact
  bundle with `scripts/bundlecheck`;
- `race`: run race-enabled tests on a supported host;
- `vet`: run Go static checks;
- `build`: compile the local `tested` executable into an ignored build
  directory;
- `crossbuild`: compile the supported OS/architecture matrix;
- `fixtures` or `reports`: run controlled end-to-end fixtures and verify the
  exact compatibility and derivative artifact set;
- `skills-check`: validate skill frontmatter, UI metadata, placeholders, and
  the complete forward-only route graph without external dependencies;
- `ci`: compose formatting verification, tests, vet, race where supported,
  cross-builds, and fixture verification without changing source;
- `clean`: remove only explicit generated roots such as `bin`, `dist`, and
  `.coverage`;
- `release-check`, `release-build`, and `release`: separate validation,
  packaging, and externally mutating publication.

Do not hide dependency installation, `go mod tidy`, license rewrites, version
changes, git staging, commits, tags, or pushes inside ordinary `test`, `build`,
or `ci` targets. Echo enough context to identify a failing stage while
preserving the underlying command status.

Keep direct formatting, vet, unit, race, and shuffled checks authoritative
until they all pass. Only then build and trust the checked-out `tested` source
for one final `-count=1` self-test. Give that run sole ownership of the root
`.coverage` directory, require coverage to be present, and run
`scripts/bundlecheck` only after a zero self-test exit so a secondary
validation error cannot mask the child status. Remove the explicit generated
root before the attempt so stale or unmanaged files cannot appear current.
Make `coverage` reuse this canonical sequence rather than launch another root
writer.

## Build CI deliberately

When adding CI, make direct Go checks the bootstrap authority before using the
new tool to test itself. A suitable job order is:

1. check out the repository and install the Go version declared by `go.mod`;
2. restore bounded dependency/build caches without caching report evidence;
3. verify formatting, then run `go test ./...` and `go vet ./...`;
4. run `go test -race ./...` and shuffled repetitions on a supported host;
5. build the current CLI, use it to test its own module once, and independently
   validate the resulting root `.coverage` inventory and manifest;
6. cross-build the CLI for the supported matrix;
7. run the built CLI against passing, failing, and build-failing fixtures;
8. verify exit precedence, file modes where the platform supports them,
   manifest checksums, exact artifact names, and deterministic rerendering;
9. upload `.coverage` only as a diagnostic artifact, including on fixture
   failure, with access and retention appropriate for potentially sensitive raw
   evidence.

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

## Handle dependencies

Prefer the standard library and existing module graph. Run `go mod download`,
`go mod tidy`, `go get`, or `go install` only when the task requires dependency
or module maintenance and network access is authorized.

After an intentional dependency change, run:

```text
go mod tidy
go mod verify
go test ./...
go test -race ./...
```

Inspect `go.mod` and `go.sum` rather than accepting unrelated upgrades.

## Prepare releases

Treat every release action as operator-controlled. Before packaging:

- require a clean intended worktree and the repository's release branch;
- validate `VERSION` as the intended semantic version and confirm the matching
  `v<VERSION>` tag does not already exist;
- run the complete CI contract;
- build reproducible named archives or binaries for the supported matrix;
- generate SHA-256 checksums and verify package contents, licenses, version
  output, and help output;
- record the exact commit, Go version, targets, and build flags without
  embedding volatile data unless the release contract requires it.

Run a non-publishing GoReleaser snapshot during tagged-release preflight using
the same pinned version and configuration as publication. Require each archive
to include `LICENSE`, `README.md`, `USER_GUIDE.md`, and `SECURITY.md`; verify
the generated checksum set and execute the native preflight binary's version
and help commands. Stamp the full RFC 3339 `.CommitDate`, not a date-only
substring, together with `.FullCommit`.

For the GitHub tag workflow, require the tagged commit to be an ancestor of
`origin/main`. Verify the exact Linux, Darwin, and Windows amd64/arm64 archive
matrix and match the extracted native binary's first version line to the
snapshot version encoded in its archive name before allowing publication.

Keep local packaging separate from tag creation and remote publication. Never
change `VERSION`, create a commit or tag, push, publish a release, or upload
artifacts unless the user explicitly requests that external state change.

The operator-controlled `make release` target follows the repository family
convention without inheriting its unsafe shortcuts. It requires a clean
`main`, the reviewed `versioned` 1.0.36 binary, a configured `origin`, and an
absent next-patch tag before running the complete `release-check`. It repeats
the clean/tag check after CI, increments only `VERSION`, creates an `ops`
commit with the required Before/After/Tests/More info sections, creates the
annotated tag, then atomically pushes only `main` and that exact tag. Do not
replace the narrow atomic push with `git push --tags`, and do not invoke the
state-changing target during validation; use a dry run or an isolated local
repository and local bare remote.

## Completion evidence

Report the exact commands run, host/matrix coverage, pass or failure results,
and generated paths. If a documented target is only proposed or not present,
say so explicitly and provide the direct command used instead.
