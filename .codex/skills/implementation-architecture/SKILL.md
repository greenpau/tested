---
name: implementation-architecture
description: Design, implement, review, or audit tested across its CLI, child-process runner, event protocol, result aggregation, coverage, reporting, and artifact-publication boundaries. Use when a change crosses packages, alters run or report lifecycle semantics, introduces a new output, or needs routing to the specialized tested implementation skills.
---

# Implementation Architecture

Develop `tested` as a lossless evidence pipeline with replaceable projections.
Keep process execution, framing, normalized results, coverage, presentation,
artifact publication, and exit policy separate while preserving one coherent
run identity.

## Implementation workflow

1. Define the invocation: command, working directory, managed output directory,
   configured Go executable, `tested` options, and exact Go argv. Prefer `--`
   as the unambiguous boundary; preserve positional pass-through after its
   first positional has selected implicit `run`. Reject ambiguous ownership
   rather than guessing.
2. Establish the evidence boundary before starting work. Create the output
   directory with mode `0700`, open raw stdout and stderr artifacts with mode
   `0600`, and arrange process-tree cancellation and pipe draining.
3. Start the child without a shell. Tee bytes to raw evidence before sending
   them to bounded framing or live presentation.
4. Decode the `TestEvent`/`BuildEvent` union and aggregate by package, test, and
   occurrence ordinal. Preserve diagnostics and finalize missing terminal
   transitions as incomplete.
5. Treat child wait status as authoritative. After raw streams close, bind that
   status to their exact sizes and SHA-256 digests in one strict, 1 MiB-bounded
   `tested/run/v1` document. Retain monotonic `duration_ns` separately from
   wall-clock timestamps. Limit bindings to the three canonical evidence names,
   require the event binding, reject duplicate JSON members at every depth,
   enforce canonical cancellation/exit mappings, and sort bindings by name when
   encoding. Record parsing, aggregation, coverage, threshold, and report
   failures separately so later failures do not erase stronger earlier
   evidence.
6. Parse coverage with Go profile semantics and statement weights. Run
   `go tool cover` with its working directory set to the tested module, not the
   report directory, then apply the fixed report-owned presentation decorator
   without changing the Go-authored source, spans, selector, or script.
7. Render all presentations from the normalized result and coverage model.
   Escape the destination syntax, apply configured redaction to presentations,
   and label measured versus estimated durations.
8. Stage applicable derived artifacts on the destination filesystem, publish
   each with an atomic rename, then publish the deterministic checksum manifest
   last only for a coherent generation.
9. Verify normal success, ordinary test failure, build failure, malformed or
   oversized framing, repeated test names, truncated output, cancellation,
   signal termination, coverage absence, threshold failure, and report failure.

## Ownership and dependency rules

- Let `pkg/cli` own syntax and validation, but let `pkg/app` own orchestration
  and exit precedence.
- Let `pkg/runner` own child lifecycle and raw streams. It must not decide test
  meaning from JSON text.
- Let `pkg/protocol` own framing and event decoding. It must not own process
  status or artifact publication.
- Let `pkg/result` own occurrence-aware semantic state. Reports must consume
  that model instead of re-parsing raw JSON.
- Let `pkg/coverage` own profile semantics, Go cover invocation, and secure
  staged decoration.
- Let `pkg/report` own escaping, redaction, deterministic projections, live
  display, the shared fixed HTML visual system, and coverage presentation
  decorator. Keep its HTML, CSS, and JavaScript sources in a compile-time
  `embed.FS`, with validated immutable assets attached to each renderer.
- Let `pkg/artifact` own managed paths, permissions, staging, atomic rename,
  digests, and manifest commit order.
- Let `pkg/runstatus` own the strict bounded schema for durable child outcome,
  cancellation, capture integrity, policy decisions, and raw-file bindings.
- Define interfaces where they are consumed, keep them small, and pass
  immutable snapshots across publication boundaries.

## Non-negotiable invariants

- `ARCH-001` — The exact child argv and selected workdir determine the Go run;
  no shell interpolation or implicit caller-directory mutation is permitted.
- `ARCH-002` — Raw stdout and stderr bytes reach secure evidence files before
  parsing or presentation. A parser or renderer failure cannot erase them.
- `ARCH-003` — Child exit, signal, and cancellation are authoritative. Parsed
  events add detail but cannot convert unsuccessful execution into success.
- `ARCH-004` — `TestEvent` and `BuildEvent` share one bounded framing stream.
  Unknown, malformed, oversized, and truncated records produce diagnostics and
  explicit incompleteness without unbounded allocation.
- `ARCH-005` — A logical test identity includes an occurrence ordinal.
  Repeated runs, retries, and repeated package/test names remain distinct.
- `ARCH-006` — Every accepted package and test reaches a terminal normalized
  state or the explicit `incomplete` state. Missing evidence is never inferred
  as pass, skip, or zero-duration completion.
- `ARCH-007` — A duration derived from an event's authoritative elapsed value
  is measured. A duration inferred from timestamps is estimated and must be
  labeled as such in machine and human projections.
- `ARCH-008` — Coverage totals equal covered statement weight divided by total
  statement weight. Package percentages are never averaged.
- `ARCH-009` — The compatibility names are exactly `test_output.jsonl`,
  `coverage.out`, `test_output.html`, and `coverage.html` under `.coverage`.
  Omit both coverage artifacts when coverage is disabled; otherwise never
  fabricate a profile and require a valid profile before generating coverage
  HTML. Preserve the Go-authored coverage source, annotation spans, selector,
  and script byte-for-byte while permitting only tested's fixed deterministic
  viewport, content-security-policy, and theme injection. Additional outputs
  are derivatives, not substitutions.
- `ARCH-010` — Managed directories use mode `0700` and managed regular files
  use mode `0600`. Paths cannot escape the managed output root through
  traversal or symlinks.
- `ARCH-011` — Derived files publish atomically and `manifest.json` publishes
  last. A manifest advertises only complete files and their actual digests.
- `ARCH-012` — The same normalized input, options, and renderer version produce
  stable ordering, identifiers, serialization, and checksums.
- `ARCH-013` — Child failure, cancellation, or signal outranks threshold and
  reporting failures. If the child succeeds, infrastructure, coverage,
  threshold, or reporting failure may make the command unsuccessful; all
  contributing causes remain inspectable.
- `ARCH-014` — Cancellation addresses the process tree, uses the default
  two-second graceful interval, forces termination when needed, drains owned
  pipes, and reaps the child before returning. It remains authoritative through
  coverage/report publication and prevents manifest publication.
- `ARCH-015` — Offline reporting trusts child outcome only when a valid
  `run.json` binds the exact event stream and every retained evidence
  companion. Explicit external evidence is copied into canonical managed names
  and stale unselected companions are removed. A bare event stream may be
  rendered for inspection but cannot imply success.
- `ARCH-016` — Coverage policy compares the exact statement ratio with the
  exact `DIGIT+("."DIGIT+)?` threshold from 0 through 100. Limit the input to
  256 bytes; equality passes. Binary floating point and rounded display
  percentages never decide the exit status.

## Specialized workflows

- Use [coding-directives](../coding-directives/SKILL.md) to implement or review tested Go code, package boundaries, errors, security, and verification style.
- Use [implementation-test-pipeline](../implementation-test-pipeline/SKILL.md) to change child execution, raw capture, event framing, occurrence aggregation, duration evidence, or exit semantics.
- Use [implementation-coverage](../implementation-coverage/SKILL.md) to change Go coverage profile parsing, merging, totals, thresholds, or canonical cover generation.
- Use [implementation-reporting](../implementation-reporting/SKILL.md) to change console output, compatibility HTML, JSON, JUnit, index, redaction, escaping, or manifest-visible report metadata.
- Use [scripts-and-automation](../scripts-and-automation/SKILL.md) to design, add, run, or document Make, CI, cross-build, and release automation.

## Acceptance boundary

Complete a cross-domain change only when focused owner tests pass, package
contracts still compose, raw evidence survives each injected downstream
failure, deterministic fixtures reproduce byte-for-byte, and exit behavior
preserves the strongest authoritative failure.
