# Profiles and Exact Thresholds

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

## Verification entrypoints

Compare [profile implementation](../../../../pkg/coverage/profile.go) with
[profile tests](../../../../pkg/coverage/profile_test.go), including duplicate,
zero-statement, overflow, and aggregate-limit cases. Compare
[threshold arithmetic](../../../../pkg/coverage/threshold.go) with
[threshold tests](../../../../pkg/coverage/threshold_test.go) for exact decimal
boundaries and unavailable coverage.
