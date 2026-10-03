---
name: scripts-and-automation
description: Operate and revise tested development and release automation. Use when selecting or changing Make commands, fixture checks, native CI, cross-builds, generated outputs, dependencies, packaging, or releases.
---

# Scripts and Automation

Own reproducible repository commands and their side effects. Inspect the
checked-out [Makefile](../../../Makefile), [module](../../../go.mod), and
[workflows](../../../.github/workflows/) before claiming a command exists or a
platform is qualified. Keep runtime semantics with their implementation owners.

Read [development, fixtures, and CI](references/verification.md) when changing
or qualifying test orchestration, toolchain/matrix coverage, evidence bundles,
or generated-output cleanup. Read [release operations](references/releases.md)
only for packaging, versioning, preflight, tags, or publication.

## Select an existing command

Run commands from the repository root unless the target sets a fixture workdir.
The current Make surface is:

| Command | Behavior and outputs |
| --- | --- |
| `make skills-check` | Check handbook metadata and the canonical route graph. |
| `make linter` | Check Go formatting and run vet; do not rewrite source. |
| `make run-tests` | Run direct package tests. |
| `make run-race-tests` | Run race-enabled package tests. |
| `make run-shuffle-tests` | Run three shuffled repetitions. |
| `make build` | Replace `bin/tested`, stamp version metadata, check version/help. |
| `make self-test` | Build, replace root `.coverage`, run with coverage and explicit `HEAD` baseline, then validate its bundle. |
| `make test` / `make coverage` | Run bootstrap checks followed by one canonical self-test. |
| `make e2e` | Build and exercise controlled fixtures, offline rerender, failures, bounds, and benchmark/metadata cases. |
| `make e2e-go126-metadata` | Exercise Go 1.26 metadata with the already-built binary; explicitly skip on older Go. |
| `make cross-build` | Compile Linux/Darwin amd64/arm64 and Windows amd64/386/arm64, including Windows test compilation. |
| `make ci` | Run skills-check, test, e2e, and cross-build. |
| `make docs` | Generate `.doc/index.txt` from Go declarations. |
| `make mod-tidy` | Intentionally change module metadata and verify modules. |
| `make clean` | Remove the explicit build, report, documentation, and fixture-output roots listed in Makefile. |
| `make release-check` | Require a clean worktree and valid version, run CI, then check cleanliness. |
| `make release` | Perform operator-requested version, commit, tag, and atomic remote publication. |

Use `gofmt -w <changed-go-files>` for intentional formatting; there is no
separate `fmt` target. For focused work, select direct `go test ./pkg/<owner>`
or a relevant `-run` expression. Do not run broad runtime suites for skill-only
edits. Do not invoke `test`, `self-test`, or `e2e` as a read-only probe: they
replace generated evidence directories.

## Preserve scope and failure evidence

Keep cleanup confined to known generated roots or owned disposable directories.
Keep dependency maintenance intentional; prefer the standard library and the
existing module graph. After an authorized module change, run `go mod tidy`,
`go mod verify`, package tests, and race checks, then inspect module diffs for
unrelated upgrades.

Do not hide dependency installation or Git mutations in test/build/CI targets.
An automation-edit request does not itself authorize running publication.
Honor existing user authorization when an external action is requested; keep
validation non-publishing and do not ask again for already authorized work.

## Acceptance and completion evidence

- A failed bootstrap test prevents self-testing from being treated as proof of
  correctness; a nonzero self-test remains the primary failure.
- A fixture bundle has the expected conditional inventory, secure modes where
  supported, exact digests, and deterministic offline derivatives.
- A foreign binary compiles without being run; native lifecycle checks provide
  the evidence for platform-specific cancellation claims.
- Cleanup leaves source, unknown files, and unrelated repositories untouched.
- Packaging/preflight does not create a release; publication follows only its
  explicitly requested operation and inspected state.

Report exact commands, observed results, host/matrix coverage, and generated
paths. Identify unrun or unavailable verification explicitly. Update affected
references when targets, pins, outputs, or side effects change.
