---
name: implementation-coverage
description: Implement and review Go coverage evidence and exact policy decisions. Use when changing profile parsing or merging, statement-weighted totals, thresholds, canonical coverage HTML, or explicit Git baseline comparisons.
---

# Implementation Coverage

Own profile semantics, exact coverage policy, selected-Go cover invocation,
and optional local source comparisons. Let the report package own visual
presentation and the app own final status precedence.

## Select the coverage operation

- Read [profiles and thresholds](references/profiles-and-thresholds.md) for
  grammar, bounded parsing, merging, weighted totals, or minimum policy.
- Read [canonical HTML](references/canonical-html.md) for raw profile handling,
  Go cover invocation, secure decoration, publication, or cleanup.
- Read [explicit source baselines](references/source-diff.md) for revision
  resolution, source mapping, bounded edit models, or Git failure behavior.

Inspect the linked implementation and tests for each affected operation. Inputs
are raw profiles, selected workdir/toolchain, optional exact decimal policy,
and an explicitly requested revision. Outputs are deterministic weighted
models, a recomputable policy decision, and applicable coverage artifacts.

## Keep evidence and policy distinct

Preserve raw profile bytes and source-path identities. Calculate integer
covered/total statement weights, never a mean of percentages. Keep disabled,
missing, empty, malformed, and genuinely zero-covered profiles distinct.

Evaluate policy only against valid available coverage. Equality satisfies the
exact threshold; display rounding never decides status. Keep a coherent
below-minimum result (exit 3) distinct from unavailable policy evidence (exit 2)
and preserve stronger child failure or cancellation.

Generate annotated HTML with the selected Go tool from the project workdir.
Preserve its source, spans, selector, and script; allow only the report-owned,
exactly removable head injection. Stage securely and retain raw evidence when
parsing, cover execution, decoration, or publication fails.

Resolve a baseline only when explicitly requested, once to an immutable local
commit. Bound every comparison dimension and fail visibly on missing objects
or ambiguous mappings. Without a baseline, neither invoke Git nor imply source
changes. Keep old and current source sensitive and unredacted.

## Verification and acceptance

Use focused profile/threshold tests for mathematical and grammar behavior,
real-toolchain cover tests for canonical bytes, local Git fixtures for source
comparison, and app tests for status and artifact coherence. Browser behavior
requires separate report verification; a valid comparison model alone does not
qualify an interactive explorer.

- Unequal file weights produce the aggregate weighted ratio, not the mean of
  their percentages.
- Duplicate `set` blocks merge as covered if any count is nonzero.
- Duplicate `count` or `atomic` blocks add counts without adding statement
  weight twice.
- Mixed modes, conflicting weights, invalid coordinates, and count overflow
  fail explicitly.
- A colon-bearing source path and a very large valid line parse correctly.
- Go-emitted zero-statement regions parse and merge without changing totals.
- Disabled, missing, empty, malformed, zero-covered, and partial coverage remain
  distinguishable in summaries and exit policy.
- An exactly representable threshold equal to the weighted ratio passes;
  finite decimals on either side of a repeating ratio compare correctly.
  Uint64-scale counts never overflow or pass through binary floating point.
- `go tool cover` resolves relative source paths from the project workdir and
  never publishes failed partial HTML.
- The fixed decorator is byte-deterministic across chunk boundaries, accepts
  the selected Go toolchain's document, rejects malformed or already-decorated
  heads, contains no external resources, and is exactly reversible by removing
  its exact rendered injection.
- A real selected-toolchain integration retains the `#files` selector,
  `pre.file` source panels, coverage spans, and change script after decoration.
- An explicit baseline resolves to one commit, includes only unambiguously
  matched current-profile files, renders deterministic added/modified/renamed
  edit scripts, and fails clearly for missing Git objects, malformed output,
  ambiguity, cancellation, and every configured bound.
- Without `--coverage-diff-base`, Git is never invoked and the report exposes
  coverage focus without fabricating source changes or a split comparison.
