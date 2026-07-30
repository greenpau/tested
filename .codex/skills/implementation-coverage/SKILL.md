---
name: implementation-coverage
description: Implement or review tested Go coverage profile parsing, deterministic merging, statement-weighted totals, per-file results, minimum thresholds, coverage artifact handling, and canonical go tool cover HTML generation. Use when changing pkg/coverage, coverage-related app behavior, coverage.out or coverage.html compatibility, or coverage tests.
---

# Implementation Coverage

Treat a Go coverage profile as structured evidence. Calculate totals from
statement weights, preserve profile modes and source coordinates, and keep
missing or invalid evidence distinct from genuine zero coverage.

## Parse the profile grammar

Require one header in the form `mode: set`, `mode: count`, or `mode: atomic`.
Parse each body record into:

- source path;
- start line and column;
- end line and column;
- statement weight;
- execution count.

Do not split a record at its first colon; source paths may contain colons.
Locate the position suffix from the structured numeric fields at the right.
Accept lines larger than scanner defaults with an explicit bounded or streaming
reader suitable for generated Go profiles. Reject malformed coordinates,
negative or overflowing values, unsupported modes, and body records before the
header with line-numbered wrapped errors. Accept and preserve zero-statement
regions emitted by the Go toolchain; they contribute zero to both weighted
totals even when their execution count is nonzero.

Apply conservative defaults of 16 MiB per logical record, 256 MiB for the raw
profile including delimiters, 100,000 unique source paths, and 1,000,000 unique
coordinate blocks. Probe at most one byte past the aggregate byte ceiling.
Repeated source paths and duplicate coordinates consume one cardinality slot,
but all of their raw bytes still consume the profile budget. Apply the file and
block defaults to in-memory merges as well as parsing, and reject negative
explicit limits.

Preserve source paths as profile identities. Clean a path only for a safe,
documented filesystem operation; do not collapse distinct profile keys or
allow a profile path to choose an output destination.

## Merge deterministically

Use source path plus exact region coordinates and statement weight as the block
identity. Sort files and blocks by their complete identity before exposing or
serializing results.

For duplicate blocks:

- merge `set` counts with logical OR;
- merge `count` and `atomic` counts with checked addition;
- reject coordinate matches whose statement weights conflict;
- reject mixed profile modes unless a future explicit conversion contract
  defines how evidence is preserved.

Count each unique block's statement weight once after merging. Never let input
order, map order, duplicate runs, or integer overflow silently change totals.

## Calculate weighted totals

For every unique block, add `NumStmt` to total statement weight and add
`NumStmt` to covered statement weight when its merged count is greater than
zero:

```text
coverage percent = 100 * covered statement weight / total statement weight
```

Apply the same calculation per file and for the aggregate. Never average file
or package percentages. Keep integer numerator and denominator in the model and
round only in a renderer using one documented rule.

Represent these cases separately:

- coverage disabled by `--no-coverage`;
- profile not produced;
- empty profile with no statements;
- malformed or unsupported profile;
- valid profile with zero covered statements;
- valid profile with partial or full coverage.

Do not call absent coverage `0%`. An empty valid profile has no percentage
unless the product contract explicitly assigns one.

## Enforce minimum coverage

Validate `--minimum-coverage` with the plain-decimal grammar
`DIGIT+("."DIGIT+)?` in the inclusive range 0 through 100 and reject inputs
longer than 256 bytes. Reject signs, whitespace, exponents, percent suffixes,
NaN, and infinities. Canonicalize only insignificant leading and trailing
zeroes.

Compare the exact weighted ratio rather than a rounded display string. Use
arbitrary-precision cross multiplication:

```text
100 * covered * decimal_denominator
    >= statements * decimal_numerator
```

Run threshold evaluation only against valid present coverage. Preserve the
exact minimum, covered/statement counts, recomputable satisfaction decision,
and a high-precision presentation value in durable run metadata.

Keep threshold failure separate from child failure and coverage parse failure.
A child failure remains authoritative; a successful child may become
unsuccessful with exit 3 because a valid total is below the requested
threshold. Treat exact equality as satisfied. Missing, empty, malformed, or
otherwise unavailable coverage under a requested policy is an
infrastructure/reporting failure with exit 2, not a below-threshold result.

## Preserve compatibility artifacts

Keep `.coverage/coverage.out` as the raw Go-produced compatibility profile.
Do not reorder, normalize, redact, or replace it merely to simplify parsing.
Create it with managed-file protections and expose its digest and size through
the manifest.

Generate `.coverage/coverage.html` with the selected Go executable:

```text
go tool cover -html=<absolute-profile-path> -o=<staged-output-path>
```

Set the command working directory to the tested project directory. Relative
source paths in a profile belong to that directory, not `.coverage` and not the
caller's unrelated current directory. Create the staged HTML on the
destination filesystem and require a successful command.

Do not substitute a custom coverage renderer. Stream the Go-authored document
through the fixed `pkg/report` presentation decorator into a second
destination-filesystem temporary file. Bound the search for the unique closing
`head`, reject a pre-existing tested theme marker within that head or multiple
closing heads, and inject only the tested-owned viewport declaration,
content-security policy, embedded theme CSS, progressive explorer JavaScript,
and optional safely encoded bounded comparison model. Do not parse, redact,
reorder, or regenerate the source, coverage spans, file selector, or
Go-authored script; the annotated source body may itself contain the marker
text. Revalidate both temporary files against replacement before publishing
the decorated file atomically with mode `0600`.

The supported Go templates use the case-sensitive `</head>` byte anchor. Limit
the buffered prefix through that anchor to 1 MiB, then stream the remaining
body with only bounded overlap needed to reject a second closing head.

The fixed assets must contain no external resource reference. Treat an optional
comparison payload as untrusted data, encode it contextually, and never admit
it through a trusted-content cast. Removing the exact rendered injection from
a completed artifact must restore the selected Go toolchain's bytes exactly.
Source the decorator's HTML fragment, shared CSS, and explorer JavaScript from
the report package's `embed.FS`; renderer construction must return an
asset-loading error rather than reading runtime files or panicking.

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

## Failure and recovery

- Preserve the profile and child status when parsing fails.
- Bound total profile bytes, source-file cardinality, block cardinality,
  individual lines, and diagnostic text for hostile profiles.
- Remove or leave unadvertised both canonical and decorated temporary outputs
  after a failed cover command or decorator; never publish a truncated file as
  complete and never replace an existing report on failure.
- Propagate context cancellation to `go tool cover` and reap it using the same
  process-lifecycle standards as other child work.
- Wrap tool stderr and exit information without leaking redacted secrets or
  discarding the underlying cause.
- Advertise coverage HTML in the manifest only after its atomic publication and
  digest calculation succeed.

## Acceptance scenarios

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
- A threshold exactly equal to a repeating weighted ratio passes, the next
  representable decimal above it fails, and uint64-scale statement counts never
  overflow or pass through binary floating point.
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
