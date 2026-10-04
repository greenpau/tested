# Packaging and Release Operations

Inspect [Makefile](../../../../Makefile),
[the shared release script](../../../../scripts/release.sh),
[the version authority validator](../../../../scripts/releaseversion/main.go),
[the tag workflow](../../../../.github/workflows/release.yml), and
[GoReleaser configuration](../../../../.goreleaser.yaml) before choosing an
operation. Keep local verification separate from authorized remote mutation.

## Prepare releases

Treat every release action as operator-controlled. Before packaging:

- require a clean intended worktree; local version/tag creation also requires
  the repository's release branch;
- validate `VERSION` as canonical `1.<minor>.<patch>`; major is always 1.
  Reject leading zeros, whitespace, multiple lines, suffixes, symlinks, and
  components outside uint64. Allow one optional final LF and reject overflow
  of the selected increment. Before local tag
  creation, require the next tag to be absent; in tagged CI preflight, verify
  that the existing annotated `v<VERSION>` tag matches the checked-out commit;
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
and help commands. Stamp `.FullCommit`, with a dirty suffix for dirty snapshots,
and identify empty/HEAD branches as detached. Use GoReleaser's full RFC 3339
`.Date` as the build timestamp; `.CommitDate` is a commit timestamp and must not
be labeled as build time. Build timestamps intentionally vary between release
invocations; keep archive modification times pinned to `.CommitTimestamp`.

For the GitHub tag workflow, require the tagged commit to be an ancestor of
`origin/main`. Verify the exact Linux, Darwin, and Windows amd64/arm64 archive
matrix and match the extracted native binary's first version line to the
snapshot version encoded in its archive name before allowing publication.

Keep local packaging separate from tag creation and remote publication. Never
change `VERSION`, create a commit or tag, push, publish a release, or upload
artifacts unless the user explicitly requests that external state change.

Route all four operator-controlled release targets through the shared script.
`make release` increments the patch; `make minor-release` increments the minor
and resets the patch to zero. Their `fast-` variants skip only the local
`release-check`; tagged CI and native preflight still gate publication. Never
claim that skipped tests passed in the generated commit message.

Require a clean release branch (default `main`), the reviewed `versioned`
1.0.36 binary, a configured remote (default `origin`), and an absent next tag.
Honor `RELEASE_BRANCH`, `RELEASE_REMOTE`, and `VERSIONED` overrides; the tagged
workflow still requires the release commit to belong to `origin/main`.
Inspect the actual push URL, refuse multiple destinations, fetch that branch,
and require the remote tip to be an ancestor of local HEAD. Compute the next
version independently in Go, probe the pinned tool in a private temporary
directory, and compare its exact result before changing the worktree. Do not
install release dependencies implicitly.

For checked releases, run the complete `release-check` once before the version
update. Recheck clean branch, HEAD, VERSION, next tag, and remote ancestry after
the gate. Increment only `VERSION`, create an `ops` commit with the required
Before/After/Tests/More info sections, verify the version-only commit and its
parent, and create an annotated tag. Push only HEAD to the release branch and
that exact tag with both `--atomic` and `--no-follow-tags`; configured
`push.followTags` must not publish unrelated tags. Keep the per-checkout release
lock through publication and clean only owned temporary files and the lock.

Keep `release-git-check` non-publishing: it qualifies the next patch version,
including the pinned tool and remote checks, and may update fetched metadata.
Retired partial update/commit targets must fail with the complete target names.
Do not invoke publication in this checkout during validation; use disposable
repositories and local bare remotes.

## Failure and acceptance

Stop on a dirty worktree, branch/version/tag mismatch, missing pinned tool,
failed preflight, or remote inspection failure. Do not bypass checks or broaden
the atomic push to recover. After a failed step, inspect the actual local and
remote state before proposing a retry; local version, commit, or tag changes
may already exist.
Preserve a failed commit/tag/push for operator inspection rather than resetting
it automatically. Refuse another increment when the current release tag differs
from its published tag or a generated current release commit has no local tag.
After a hard interruption, inspect an existing `.git/tested-release.lock`
before removing it; do not delete another active release's lock.

Verify release mechanics in a disposable local repository with a local bare
remote. A failed check must prevent subsequent mutation; a successful flow
must touch only the intended version, commit, branch, and exact tag. Never
publish a real release merely to validate this guidance.
