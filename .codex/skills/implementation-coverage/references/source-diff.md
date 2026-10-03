# Explicit Local Source Baselines

## Compare source only from an explicit baseline

Enable source changes only with `--coverage-diff-base REV`. Validate the
revision as one bounded argument, resolve it once to a full immutable commit
object with local Git, and use that object identifier for every comparison.
Do not infer `HEAD`, a default branch, a GitHub event, or a merge base. Do not
fetch, contact a remote, run a shell, enable external diff drivers, or apply
text-conversion filters. Make Git a dependency only for the explicit option.

Resolve each profile source to a repository file unambiguously using the
selected project working directory and Go package identity. Never attach a
suffix-guessed baseline to an ambiguous profile name. Compare only files in
the current coverage profile: support modified, added, renamed, and untracked
current files, and omit deleted-only files because no current coverage panel
exists. Record old and new repository paths and a deterministic structured
zero-context edit script. Preserve deleted line text for offline rendering;
unchanged gaps may be reconstructed only when line mappings and current source
validation agree.

Bound revision text, command output and diagnostics, files, per-file and
aggregate source/diff bytes, hunks, lines, and line length. Propagate
cancellation through every owned Git process and reject missing commits,
shallow-clone omissions, ambiguous mappings, malformed Git output, source
mutation, and exceeded bounds without publishing a misleading comparison.
Keep coverage reporting unchanged when no baseline is requested. Treat
baseline source as sensitive unredacted coverage content because it can reveal
secrets removed from current source.

## Verification entrypoints

Inspect [baseline construction](../../../../pkg/coverage/diff.go),
[structured comparison](../../../../pkg/coverage/diff_build.go), and
[diff tests](../../../../pkg/coverage/diff_test.go). Qualify modified, added,
renamed, untracked, ambiguous, unavailable, cancelled, and bounded cases with
local repositories; do not fetch missing objects as test setup or recovery.
