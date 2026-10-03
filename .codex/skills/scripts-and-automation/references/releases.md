# Packaging and Release Operations

Inspect [Makefile](../../../../Makefile),
[the tag workflow](../../../../.github/workflows/release.yml), and
[GoReleaser configuration](../../../../.goreleaser.yaml) before choosing an
operation. Keep local verification separate from authorized remote mutation.

## Prepare releases

Treat every release action as operator-controlled. Before packaging:

- require a clean intended worktree; local version/tag creation also requires
  the repository's release branch;
- validate `VERSION` as the intended semantic version. Before local tag
  creation, require the next tag to be absent; in tagged CI preflight, verify
  that the existing `v<VERSION>` tag matches the checked-out commit;
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

## Failure and acceptance

Stop on a dirty worktree, branch/version/tag mismatch, missing pinned tool,
failed preflight, or remote inspection failure. Do not bypass checks or broaden
the atomic push to recover. After a failed step, inspect the actual local and
remote state before proposing a retry; local version, commit, or tag changes
may already exist.

Verify release mechanics in a disposable local repository with a local bare
remote. A failed check must prevent subsequent mutation; a successful flow
must touch only the intended version, commit, branch, and exact tag. Never
publish a real release merely to validate this guidance.
