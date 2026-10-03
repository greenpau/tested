# Outcome, Timing, and Metadata Projections

## Preserve outcome and timing semantics

Render package/test occurrence identity explicitly. Never collapse repeated
occurrences in HTML, JSON, JUnit, or slowest lists.

Respect `--max-test-output-bytes` when embedding occurrence output. Report the
original and retained byte counts and an explicit truncation marker in machine
and human projections. Default the bound to 1 MiB per occurrence, interpret
zero as unlimited, and keep raw JSONL byte-faithful regardless of the
presentation bound.

Respect the aggregate `--max-total-output-bytes` budget as well. Renderers may
stream or create artifact-specific views, but must not rebuild multiple
unbounded copies of every retained transcript. Preserve aggregate byte and
truncation facts in machine output.

Preserve aggregate normalized-entry and normalized-string counts in machine
output. Treat exhausted semantic budgets as incomplete evidence, while
distinguishing ordinary derived-output clipping from semantic loss.

Project normalized Go 1.25 attributes and Go 1.26 artifact-directory records
with their source sequence and package/test occurrence. Apply presentation
redaction to attribute keys, values, and artifact paths before JSON, HTML, or
JUnit serialization. Render child-provided artifact paths as informational
text only; never create a link or resolve, open, copy, or publish the path.

Project states consistently:

- `pass` is successful;
- `fail` is a test failure;
- `skip` is skipped, not successful execution;
- `incomplete` is an error/indeterminate outcome, never pass or skip;
- build failures remain visible even without test cases;
- child cancellation, signal, start, wait, and capture failures remain run
  failures regardless of individual event outcomes.
- an unmet exact coverage policy is a distinct `coverage_failed` outcome after
  a successful, coherent child run.

When no ordinary test case can carry a run-level failure, add a deterministic
synthetic JUnit case. Cover nonzero child exit with an empty stream, unknown
child status, cancellation, fatal infrastructure/capture issues, stream
corruption, and an unmet coverage policy so CI cannot display a false-green
suite.

Use measured event elapsed values when present. Label timestamp-derived values
as estimated in JSON and human reports. In JUnit, use the best numeric duration
only when required for compatibility and add deterministic metadata that
identifies its quality; omit duration when no evidence exists.

Do not claim that summed test times equal wall-clock duration. Report the child
wall-clock span separately.

## Verification entrypoints

Inspect [assessment](../../../../pkg/report/assessment.go),
[summary](../../../../pkg/report/summary.go), and
[JUnit](../../../../pkg/report/junit.go). Use
[report tests](../../../../pkg/report/report_test.go) and
[the summary golden](../../../../pkg/report/testdata/summary.golden.json)
for truthful failure, duration-quality, budget, and occurrence projections.
Use app/CLI fixture evidence when a new protocol field crosses the whole run.
