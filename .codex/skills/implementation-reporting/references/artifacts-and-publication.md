# Evidence Bundles and Publication

## Maintain the artifact set

Preserve exactly these compatibility artifacts under the managed output
directory:

| Artifact | Contract |
| --- | --- |
| `test_output.jsonl` | Exact captured Go stdout evidence; write before parsing and never redact or normalize. |
| `coverage.out` | Exact Go coverage profile evidence; keep absent when coverage is explicitly disabled. |
| `test_output.html` | Self-contained human test report compatible with the established `go-test-report` use case. |
| `coverage.html` | Selected toolchain's Go-authored annotated report plus tested's fixed, self-contained presentation layer. |

Omit `coverage.out` and `coverage.html` after a successful generation when
coverage is explicitly disabled. If coverage evidence is absent or invalid,
never fabricate a profile; preserve any nonempty invalid profile as evidence
and omit only derivatives that could not be produced.

Add these managed outputs without substituting for the compatibility set:

| Artifact | Contract |
| --- | --- |
| `stderr.log` | Exact captured child stderr evidence; secure it like raw JSONL. |
| `run.json` | Strict bounded child outcome, cancellation, capture-integrity, issue, coverage-policy, and raw-file binding evidence. |
| `summary.json` | Versioned deterministic machine summary with run, outcome, occurrence, duration-quality, coverage, diagnostic, and artifact facts. |
| `junit.xml` | CI projection with one stable test case per occurrence and explicit fail, skip, error/incomplete semantics. |
| `index.html` | Self-contained escaped landing page linking only to managed reports that exist. |
| `manifest.json` | Conditional, last-published deterministic inventory of the completed generation, including sizes, SHA-256 digests, media roles, and sensitivity. |

Treat live-captured and explicitly imported evidence as durable during report
regeneration. Copy external event, stderr, and profile bytes without
transformation into their fixed managed names before reporting. Canonically
re-encode a valid external `run.json`, while preserving the bytes of a status
file already at the managed path. The exception is a successful, policy-free
`report --no-coverage` projection: remove a `coverage.out` binding and
canonically rewrite managed status while preserving every authoritative child
field, issue, and retained binding. Refuse `report --no-coverage` before
artifact preparation when selected status records a coverage policy; do not
erase that policy, turn its outcome green, or mutate the existing bundle. The
`report` command may replace derived reports atomically, but must not truncate
or reinterpret the selected event, stderr, or coverage bytes.

Require matching `run.json` metadata before an offline report can claim success
or publish a coherent manifest. Verify every retained evidence file against its
bound size and SHA-256. Import bound same-named stderr/profile siblings from an
explicit external status bundle when no explicit override is provided. Remove
stale managed evidence companions that are not selected or bound, without
removing unknown files. Do not borrow default status for custom events. A bare
event stream remains useful for inspection, but its process outcome is unknown
and the rendered run is incomplete.

Before chmodding or hashing a retained managed regular file on POSIX, require a
link count of one and fail closed when link-count metadata is unavailable.
Reject multiply-linked managed evidence and reports so tested cannot change an
external alias or admit it to the manifest. Treat explicitly selected external
hard links as read-only import sources: atomic copy publication must create an
independent managed inode. An explicitly omitted coverage profile may be
unlinked by its managed name without chmodding or hashing an external alias.

## Publish securely and atomically

Create managed directories with `0700` and managed files with `0600`. Stage
each derivative in a new regular file on the destination filesystem, flush and
close it, set its final mode, and atomically rename it over the managed target.
Refuse symlink or path traversal targets.

Render all requested derivatives before committing the manifest. After each
file is published, hash the actual final bytes. Publish `manifest.json` last
and include only files whose rename, mode, size, and digest checks succeeded.
Consumers must treat a missing manifest or digest mismatch as an incomplete
generation.

Make artifact presence conditional. Link only to managed files that exist.
Withhold or remove the manifest after cancellation, malformed or unbound
evidence, incomplete capture/result state, contradictory child/event status,
or any requested derivative failure. Permit a coherent authoritative
test/build failure or exact coverage-policy failure to publish a manifest.

On a report failure:

- retain raw evidence;
- do not advertise a partial derivative;
- clean safe temporary files or leave them outside the manifest for recovery;
- preserve any child failure as the primary run status;
- fail an otherwise successful `run` or `report` command when a requested
  report cannot be rendered or published.

## Verification entrypoints

Inspect [artifact storage](../../../../pkg/artifact/),
[report publication tests](../../../../pkg/report/report_test.go), and
[offline app tests](../../../../pkg/app/application_test.go) for the composed
bundle. Use [manifest tests](../../../../pkg/artifact/manifest_test.go) and
[layout tests](../../../../pkg/artifact/layout_test.go) to qualify secure
retention, independent imports, and conditional inventory. The pipeline's
status contract owns strict schema and final exit policy.
