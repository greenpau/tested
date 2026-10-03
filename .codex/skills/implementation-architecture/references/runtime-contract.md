# Runtime Contract

Apply this contract when changing CLI ownership, run/report orchestration, or
cross-package lifecycle behavior. Specialized skills own the detailed
algorithms and verification at each boundary.

## Product responsibilities

- Parse the `tested` command line without rewriting arguments intended for the
  Go tool. Support the default/`run`, `report`, `version`, and help surfaces;
  prefer Go arguments after `--` as the unambiguous form. Also preserve the
  intentional shorthand in which the first positional starts Go-argument
  pass-through, such as `tested ./... -run TestLogin`. Keep `-C`/`--work-dir`,
  `-o`/`--output-dir`, `--go`, `--title`, `--no-coverage`,
  `--minimum-coverage`, `--format plain|markdown|json`,
  `--coverage-diff-base`,
  `--color auto|always|never`, `--quiet`, `--slowest`,
  `--max-event-bytes`, `--max-test-output-bytes`,
  `--max-total-output-bytes`, `--max-result-entries`,
  `--max-normalized-bytes`, and repeatable `--redact` as tested-owned
  options. Let `report` additionally select `--events`, `--stderr`,
  `--coverprofile`, `--run-metadata`, and `--allow-failures`.
- Resolve a working directory independently from an output directory. Run Go
  commands and `go tool cover` in the selected working directory so package and
  source paths retain their Go-module meaning.
- Start the child from an argv vector, stream stdout and stderr without shell
  interpolation, persist raw evidence before interpreting it, and propagate
  cancellation to the complete child process tree.
- Decode a bounded, tolerant union of `TestEvent` and `BuildEvent` records.
  Preserve unknown or malformed input as diagnostics and represent missing
  terminal evidence explicitly as incomplete. Recognize Go 1.25 `attr` and Go
  1.26 `artifacts` TestEvents, retain their typed fields and exact raw record,
  and bind them to the source package/test occurrence. Project normalized
  metadata into JSON, HTML, and JUnit only after redaction, and keep child
  artifact paths as non-clickable informational text.
- Normalize packages, tests, repeated occurrences, durations, diagnostics,
  build failures, and coverage into one result model. Never merge repeated test
  names merely because their package and name match. Bound report-retained
  output per scope and in aggregate, normalized cardinality, and dynamically
  retained normalized strings, without truncating raw evidence.
- Stream normalized test/package/build progress and captured log previews in
  plain and Markdown modes, along with run/report processing stages. Keep
  `--quiet` final-only and JSON mode a single final summary. Own a joined
  periodic status worker in app orchestration; report the current stage,
  elapsed time, and record count after ten seconds without displayed progress
  (checked once per second). Suppressed detail must not postpone that status.
  Keep offline analysis progress separate from historical test execution.
- Compute coverage from statement weights in the profile. Never average
  package percentages, and never infer coverage from terminal text.
- Keep `index.html`, `test_output.html`, and `coverage.html` within one compact,
  responsive, keyboard-accessible visual system that honors system dark mode
  and print. Preserve the Go-authored coverage source, spans, selector, and
  script byte-for-byte apart from tested's exactly removable head injection.
  Let the embedded coverage explorer add package filtering, baseline-dependent
  changed-file narrowing, bounded coverage-focused regions, and expandable
  gaps without replacing the canonical fallback. Enable genuine unified/split
  source changes only when `--coverage-diff-base` explicitly resolves to an
  immutable local Git commit; never infer or fetch a baseline. Keep every
  report-owned HTML, CSS, and JavaScript source under `pkg/report/assets/` and
  compile it through Go's `embed` package; do not use runtime asset paths or
  restore large markup/style/script literals to Go files.
- Preserve these compatibility artifact names exactly when applicable:
  `.coverage/test_output.jsonl`, `.coverage/coverage.out`,
  `.coverage/test_output.html`, and `.coverage/coverage.html`. Omit the
  coverage pair after a successful generation when coverage is disabled. In
  report mode, project a policy-free bound status without its coverage binding,
  but reject `--no-coverage` before mutation when status records a coverage
  policy. Never fabricate a missing profile; retain a nonempty invalid profile
  as evidence but omit coverage HTML that cannot be generated.
- Add `run.json`, `summary.json`, `manifest.json`, `junit.xml`, `stderr.log`,
  and `index.html` as secure managed artifacts. Bind child status in `run.json`
  to the exact evidence sizes and SHA-256 digests under the strict bounded
  `tested/run/v1` schema, including monotonic `duration_ns`; accept no more than
  the three canonical evidence names, reject duplicate JSON members at every
  depth, enforce canonical cancellation/exit mappings, and sort bindings on
  encode. Escape format-specific content, apply configured redaction only to
  presentations, and keep live-captured or imported evidence access-limited.
- Publish derived files atomically and the manifest last. Make output ordering,
  identifiers, serialization, and checksums deterministic for the same
  normalized input and options. On POSIX, reject multiply-linked retained
  managed files before chmod or hashing; import external hard links by atomic
  copy to an independent managed inode.
- Keep test failure, infrastructure failure, report failure, threshold failure,
  cancellation, and signal termination distinguishable. Apply documented
  precedence while preserving the authoritative child status.

## Canonical run lifecycle

1. Parse `tested` options and separate the explicit or positional-pass-through
   Go argv; resolve absolute working and managed output directories without
   changing the caller's process directory.
2. Create managed output storage with mode `0700`, create raw evidence files
   with mode `0600`, and establish cancellation and process-tree ownership.
3. Start the configured Go executable with an argv vector. Capture stdout to
   `test_output.jsonl` and stderr to `stderr.log` before either stream enters a
   parser or live renderer. In report mode, copy explicitly selected external
   evidence into those canonical managed names and remove stale managed
   companions that do not belong to the selected bundle.
4. Frame bounded records, decode `TestEvent` or `BuildEvent`, and aggregate an
   occurrence-aware normalized result while live output remains a projection.
5. Wait for and reap the complete child. On cancellation, interrupt the process
   tree, allow the default two-second grace period, then force termination and
   continue draining owned pipes. Apply cancellation through report generation
   and withhold a coherence manifest for a cancelled command.
6. Finalize open packages and tests as explicitly incomplete. Mark durations as
   measured or estimated according to their evidence.
7. Parse and combine coverage profiles using weighted statement totals. Accept
   minimums of at most 256 bytes and only in the exact
   `DIGIT+("."DIGIT+)?` grammar from 0 through 100, and treat equality as
   satisfied. Compare the exact decimal ratio without binary floating point or
   display rounding. Invoke `go tool cover` from the run working directory for
   its annotated HTML, then stream it through the report-owned fixed
   presentation decorator before atomic publication.
8. Persist `run.json` only after raw files close, with explicit start, exit,
   signal, cancellation, capture-integrity, issue, coverage-policy, and raw-file
   binding fields. Never infer offline success from an unbound event stream.
9. Stage escaped and redactable derivatives, atomically publish each completed
   file, and publish the checksum manifest only after the applicable
   conditional artifact set is coherent and complete. Never apply redaction to
   event, stderr, coverage, status, or manifest-integrity evidence.
10. Select the final status by documented precedence. A reporting problem may
   fail an otherwise successful run, but it may not replace or hide a child
   test failure, signal, or cancellation.

