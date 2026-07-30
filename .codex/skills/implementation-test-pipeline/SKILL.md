---
name: implementation-test-pipeline
description: Implement or review tested child-process execution, raw stream capture, bounded TestEvent and BuildEvent framing, occurrence-aware result aggregation, timing, incompleteness, cancellation, and exit behavior. Use when changing pkg/runner, pkg/protocol, pkg/result, run orchestration, or tests for Go event handling and child failures.
---

# Implementation Test Pipeline

Preserve the Go command as the authority and the byte streams as evidence.
Treat decoded events and live output as downstream interpretations that may add
detail but may not rewrite execution history.

## Execute and capture

1. Accept the Go executable, working directory, environment policy, and argv as
   separate values. Preserve every Go argument after `--` exactly and avoid a
   shell.
2. Prepare secure evidence files before starting the child. Create rather than
   follow evidence destinations and refuse unsafe managed paths.
3. Tee stdout bytes to `test_output.jsonl` before protocol framing. Tee stderr
   bytes to `stderr.log` before diagnostic or console processing. Propagate
   write failures to orchestration without discarding child status.
4. Stream incrementally with bounded buffers and backpressure. Do not hold the
   complete run in memory merely to decode or display it.
5. Wait for the child and drain owned streams. Preserve numeric exit status,
   signal termination, start failure, wait failure, and cancellation as
   distinct evidence.
6. After raw files close, persist a bounded `run.json` that binds the status to
   the exact event, stderr, and optional coverage bytes by size and SHA-256.
   Record whether the child started, whether exit status is known, whether
   capture completed, the monotonic child span as `duration_ns` when available,
   and every fatal run-integrity issue. Encode one strict `tested/run/v1`
   document no larger than 1 MiB; reject duplicate JSON members at every depth,
   unknown fields, trailing JSON values, inconsistent cancellation/timing
   state, duplicate or unmanaged bindings, and a missing `test_output.jsonl`
   binding. Accept only interrupt→130 and terminated→143 for signal
   cancellation. Permit at most the three canonical evidence names and sort
   bindings lexically when encoding.

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

## Cancel the complete process tree

Give the child a process-group or platform-equivalent ownership boundary. On
context cancellation:

1. mark the run cancellation cause;
2. request graceful termination for the owned process tree;
3. continue draining stdout and stderr;
4. after a bounded grace interval, force termination of remaining descendants;
5. wait, reap, close streams, and finalize active results as incomplete.

Use a two-second graceful interval when no internal override is configured.
Bound post-exit pipe draining and forced-termination waits as well as the first
signal interval.

Handle races between natural exit and cancellation idempotently. Never signal a
reused process identifier after ownership has ended, and never return while an
owned child or pipe goroutine can still mutate artifacts.

Use a non-reaping exit observer where the platform provides one. Commit the
wait-state transition that disables process-tree signaling before `Cmd.Wait`
releases the stable child identity. On Windows, observe the process handle
becoming signaled before reaping and retain an assigned Job Object through
post-wait cleanup. If the observer fails, fail closed by terminating the owned
tree before disabling signaling and reaping, and bound that fallback reap in
case termination also fails. Never pass a reaped numeric PID to `taskkill`. If
Job Object assignment fails, preserve the ownership error and make only a
bounded pre-reap `taskkill` attempt while the original process handle still
reserves the leader PID. After reaping, use only the stable Job Object and
preserve any ownership or cleanup error.

Treat test-helper control files as bounded protocols. Publish content-bearing
records completely to a same-directory temporary file and atomically rename
them into view; require an explicit completion delimiter and retry absent,
empty, or unterminated observations until a deadline. Use existence-only
markers only when their contents are irrelevant and install the guarded state
before creating them. On setup failure, cancel and join the helper, and disable
numeric-PID cleanup immediately after observing that the process is gone. Have
the parent signal a blocking helper and bound its wait; do not let a helper race
asynchronous self-signal delivery against a fallback exit.

Classify programmatic cancellation, deadline expiry, SIGINT, and SIGTERM
separately. Preserve the observed child status, but project cancellation to the
documented shell result (`130` for programmatic/deadline/SIGINT and `143` for
SIGTERM). Apply cancellation precedence through coverage and report generation,
not only while the Go child is running, and remove or withhold the manifest
when cancellation prevents a coherent publication.

## Apply failure precedence

Preserve all causes, then select command status in this order:

1. cancellation (`130` for programmatic, deadline, or SIGINT; `143` for
   SIGTERM);
2. authoritative nonzero child exit or signal projection, preserved exactly
   unless coherent offline `--allow-failures` permits exit 1;
3. start, wait, raw capture, protocol, result-integrity, or incomplete-evidence
   failure (`2`);
4. unallowed failing event evidence (`1`), except that failure evidence
   conflicting with a zero child exit is an integrity failure (`2`);
5. an exact unmet minimum-coverage policy after a coherent successful child
   (`3`);
6. report rendering or artifact-publication failure (`2`);
7. success (`0`).

Use platform-appropriate signal or exit projection when possible. If several
failures share a run, keep the authoritative child status and attach later
failures to diagnostics and summary metadata. A parsed `fail` with a zero child
exit is an integrity conflict, not permission to report success.

The `report` command has no new child status. Copy explicitly selected external
event, stderr, and coverage evidence into their canonical managed names before
analysis. Import bound same-named stderr/profile siblings from an explicitly
selected external status bundle when no explicit companion overrides them.
Remove stale managed companions that are not selected or bound, while leaving
unknown files untouched.

Trust imported status only when the selected strict `run.json`
cryptographically binds the selected evidence. Re-encode an external valid
status document canonically at the managed `run.json` path, but do not rewrite
a status file already at that exact managed path during an ordinary rerender.
For a policy-free `report --no-coverage`, remove the profile and its binding,
canonically rewrite managed status only when that projection changes it, and
preserve all child/cancellation/issue fields and retained evidence bindings.
If status records a coverage policy, reject `report --no-coverage` before
artifact preparation so the command cannot erase an authoritative decision or
mutate the bundle. Render a bare log as incomplete for inspection, return
infrastructure failure, and withhold the manifest. Never consume an unrelated
default `run.json` for an explicit custom event stream.

## Acceptance scenarios

- A passing run streams raw JSONL, produces measured results, and returns zero.
- A normal `go test` failure still generates reports and returns the child's
  nonzero status.
- A compile failure represented by text or `BuildEvent` appears in normalized
  build failures even without a completed test.
- Two occurrences of the same package and test name have different ordinals
  and independent outcome/duration/output.
- A malformed or oversized record remains in raw evidence, yields a bounded
  diagnostic, and leaves affected active state incomplete.
- Output beyond the per-occurrence report bound is marked as truncated while
  the raw JSONL remains byte-for-byte complete.
- A helper publishes a complete PID record atomically; partial observations
  cannot trigger parsing, leaked goroutines, or reused-PID signaling.
- Cancellation terminates descendants, drains both streams, reaps the child,
  and returns cancellation even if rendering later fails.
- A renderer failure after a child test failure is reported but does not
  replace the child failure status.
