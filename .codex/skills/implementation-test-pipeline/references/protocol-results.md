# Protocol and Occurrence Results

## Frame a tolerant bounded protocol

Decode a tagged union with these semantic variants:

- `TestEvent` carries Go `test2json` package/test actions, timestamps, elapsed
  values, and output.
- Go 1.25 `attr` and Go 1.26 `artifacts` are known `TestEvent` actions. Decode
  and retain their `Key`/`Value` or `Path` fields and exact raw record. Bind
  each record to the source package or active test occurrence and retain its
  source sequence for deterministic, occurrence-aware projections.
- `BuildEvent` carries non-test build or tool diagnostics that remain relevant
  even when no package/test terminal action appears.

Accept chunk boundaries at arbitrary byte positions. Tolerate CRLF, blank
records, a final record without a newline, and non-JSON build/diagnostic text.
Apply `--max-event-bytes` before allocating or decoding a complete record.
Default the limit to 16 MiB, require a nonzero override to be at least 1 KiB,
and treat zero as an explicit opt-out. When a record exceeds an active bound,
discard or spool only according to the documented bounded algorithm, emit one
bounded diagnostic for that record, and resume at the next framing boundary.
Within an otherwise bounded record, permit at most 64 fields unknown to the
selected schema and 64 KiB total decoded unknown-field names plus exact
retained JSON values. Reject duplicate decoded object members at every retained
depth, including escaped-equivalent names. Treat extension exhaustion and
duplicate members as malformed semantic evidence without changing raw capture.

Never substitute tolerance for truth:

- Preserve every input byte in raw evidence even when decoding rejects it.
- Associate malformed, unknown, oversized, and truncated data with diagnostics
  rather than inventing a test action.
- Bound diagnostic count and retained text as well as frame size so adversarial
  output cannot cause a second memory expansion.
- Close or finish the stream explicitly so an unterminated final record is
  handled once and active semantic state can be finalized.

## Aggregate occurrences

Model a test occurrence independently from its display name. Use package,
logical test name, and a monotonically assigned occurrence ordinal as the
stable identity. Allocate a new ordinal whenever a new run begins after a
terminal occurrence with the same package and name; do not merge retries or
repeated invocations.

Apply `--max-test-output-bytes` independently to the output retained for each
normalized test occurrence, package, build, and unattributed-output scope.
Retain a deterministic prefix or other documented bounded projection plus the
original and retained byte counts and an explicit truncation marker. Default
to 1 MiB for each scope, require nonzero overrides to be at least 1 KiB, and
interpret zero as unlimited. Never apply this bound to `test_output.jsonl`.

Also enforce run-wide retained-output, normalized-entry, and normalized-string
budgets. The defaults are 64 MiB, 1,000,000 entries, and 128 MiB,
respectively; zero explicitly disables any bound. Count packages, occurrences,
builds, metadata records, unknown-action summaries, retained output chunks,
and dynamically retained identity/metadata strings so many individually small
records cannot bypass aggregate memory policy. Keep output text under its
separate output budgets. When semantic entries or strings must be dropped,
retain raw evidence, emit one bounded integrity diagnostic, and make the
derived result incomplete rather than silently undercounting it.

Apply package and test transitions in event order:

- Record `run` as the start of one occurrence.
- Record output with source order and owning package/test when known.
- Recognize a Go benchmark result containing only its benchmark name and
  iteration count as terminal benchmark evidence. Go omits the metric pairs
  when the measured `ns/op` is exactly zero. Reassemble the canonical,
  literal-tab benchmark-name prefix with subsequent same-package,
  same-test-scope output portions through their line ending when `test2json`
  exposes one result as multiple events. Attribute the prefix immediately to a
  provisional benchmark occurrence and account every portion under the normal
  output budgets in source order. Bound aggregate assembly bytes and fragments;
  retain an explicitly incomplete provisional occurrence when its scope
  changes, an unscoped event makes continuity unverifiable, its completion is
  malformed or missing, or capacity is exhausted.
  Resolve an already observed exact benchmark identity before interpreting a
  trailing numeric `-N` as a CPU suffix and falling back to its base identity.
  Preserve an occurrence that already had independent terminal `bench`
  evidence.
  Capacity exhaustion also makes the derived result incomplete and emits one
  bounded diagnostic. Preserve every original chunk on a successfully assembled
  occurrence. Repeated result lines must still become distinct occurrences.
  Keep independent terminal `bench` evidence authoritative whether it arrives
  before or after an interrupted partial result line. When `test2json` emits a
  complete package-scoped result before test-scoped benchmark logs and the
  terminal `bench` action, accept exactly one matching terminal action as
  corroborating the already completed occurrence; a duplicate remains an
  integrity violation. `test2json` can also omit `run` when a benchmark report
  and its test-scoped log output precede `bench`, `fail`, or `skip`. Allow those
  three terminal actions to finish an inferred benchmark evidence occurrence
  with nonempty test-scoped output; keep ordinary orphan terminals and a
  benchmark `pass` invalid.
- Accept `pass`, `fail`, and `skip` as terminal event outcomes.
- Preserve package terminal state separately from child process state.
- Preserve build failures even if no corresponding package event completes.
- Treat contradictory or impossible transitions as diagnostics and keep the
  strongest observed failure evidence.

At EOF, parser termination, cancellation, or child termination, finalize every
active package and test as `incomplete`. Do not count incomplete as pass, fail,
or skip, and do not omit it from machine reports.

Return immutable, deterministically sorted snapshots. Keep analyzer mutation
private so renderers cannot change aggregation state.

## Represent time honestly

Prefer a valid terminal event `Elapsed` value as the measured duration for that
package or occurrence. When elapsed is absent but compatible event timestamps
bound a lifecycle, compute an estimate and mark its quality as `estimated`.
When neither exists, leave duration absent.

Keep these values distinct:

- child wall-clock run span;
- event-reported measured duration;
- timestamp-derived estimated duration;
- no duration evidence.

Do not sum overlapping test durations to claim wall-clock time. Renderers must
carry duration quality into JSON/JUnit metadata and human labels where the
difference matters.

## Verification entrypoints

Inspect [protocol decoding](../../../../pkg/protocol/event.go),
[framing tests](../../../../pkg/protocol/stream_test.go),
[result aggregation](../../../../pkg/result/analyzer.go),
[occurrence tests](../../../../pkg/result/analyzer_test.go), and
[resource-budget tests](../../../../pkg/result/resource_budget_test.go).
Use metadata integrity tests for orphan or repeated attr/artifact records;
use the controlled CLI fixture when qualifying selected-toolchain event shape.
