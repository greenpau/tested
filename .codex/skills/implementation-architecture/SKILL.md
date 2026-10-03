---
name: implementation-architecture
description: Design and review tested package contracts and route implementation or automation work. Use when changing CLI or run/report behavior, crossing package boundaries, or selecting a specialized runtime, reporting, coverage, or CI workflow.
---

# Implementation Architecture

Keep `tested` a lossless evidence pipeline with replaceable presentations.
Own CLI/app composition and shared lifecycle rules here; delegate algorithms,
report formats, and operating procedures to their focused owners.

## Select the work and evidence

For runtime or CLI work, read [the runtime contract](references/runtime-contract.md)
for option ownership, compatibility artifacts, and the canonical run/report
lifecycle. Automation-only work can go directly to its specialized route.

Start with the invocation and observable result: working/output directories,
selected Go executable, tested options, exact Go argv, retained evidence,
derivatives, and final status. Trace the affected boundary through
[CLI parsing](../../../pkg/cli/options.go),
[app orchestration](../../../pkg/app/), and the owner below. Do not infer
success from events or presentation output when process evidence is missing.

1. Locate the owning behavior and inspect source plus representative test
   assertions before changing the contract.
2. Preserve raw capture, authoritative child outcome, occurrence identity,
   bounded normalization, and explicit incompleteness across the change.
3. Keep status binding, coverage, rendering, and atomic publication in lifecycle
   order. Propagate cancellation through all of them and withhold a cancelled
   generation's manifest.
4. Follow only the specialized routes needed by the task.
5. Verify the changed boundary and its integration. Report observed evidence
   separately from checks that were not run or require another native host.
6. Before the turn's final response, review the final code diff and update the
   owning repo-local skills and references accordingly. Apply the end-of-turn
   maintenance requirement even for partial work; report updates or explain
   why existing guidance needed no change.

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
  staged decoration. Let it also own explicit Git-baseline resolution and the
  bounded structured source comparison used by the coverage explorer.
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

## Specialized workflows

- Use [coding-directives](../coding-directives/SKILL.md) to implement or review Go design, API ownership, errors, concurrency, serialization classification, or verification style.
- Use [implementation-test-pipeline](../implementation-test-pipeline/SKILL.md) to change execution, raw capture, event framing, occurrences, duration evidence, bound run status, offline evidence selection, or exit semantics.
- Use [implementation-coverage](../implementation-coverage/SKILL.md) to change coverage profile parsing, merging, weighted totals, exact thresholds, Go cover generation, or explicit local Git baselines.
- Use [implementation-reporting](../implementation-reporting/SKILL.md) to change console or artifact projections, HTML assets, redaction, escaping, managed storage protections, atomic publication, or manifests.
- Use [scripts-and-automation](../scripts-and-automation/SKILL.md) to select, run, change, or document Make, fixture, CI, cross-build, packaging, or release workflows.

## Verification and acceptance

Inspect [CLI tests](../../../pkg/cli/options_test.go) for argv boundaries and
validation, [application tests](../../../pkg/app/application_test.go) for
composed run/report behavior, and [exit tests](../../../pkg/app/exit_test.go)
for precedence. Owner tests qualify their narrower boundaries; fixture and
native-host checks qualify integration, not merely compilation.

- Explicit `--` and positional pass-through preserve the Go argv; working
  directory and output directory resolve independently without changing the
  caller's directory.
- A successful child plus coherent evidence and requested derivatives returns
  zero; a downstream failure cannot erase a child test failure or cancellation.
- A parser, coverage, or renderer failure leaves raw evidence inspectable and
  prevents a manifest that falsely claims a complete generation.
- A bound offline rerender reproduces deterministic derivatives; a bare event
  stream can be inspected but cannot establish success.
- An automation-only request reaches the automation skill without requiring
  unrelated parser or renderer changes.
