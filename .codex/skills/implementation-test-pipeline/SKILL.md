---
name: implementation-test-pipeline
description: Implement and review tested execution, event normalization, and authoritative outcomes. Use when changing runner, protocol, result, runstatus, cancellation, offline evidence selection, or exit precedence.
---

# Implementation Test Pipeline

Own the transition from the selected Go command and raw streams to bounded,
occurrence-aware results and durable child status. Keep presentation and
coverage algorithms with their owners. Parsed events may explain execution;
they cannot rewrite its outcome.

## Choose the affected boundary

| Task | Required detail |
| --- | --- |
| Child argv, capture, pipes, cancellation, process-tree cleanup, helper synchronization | Read [execution and cancellation](references/execution.md). |
| Framing, union decoding, budgets, repeated tests, benchmark assembly, metadata or durations | Read [protocol and occurrence results](references/protocol-results.md). |
| Durable status, run/report exit policy, external evidence import or coverage-policy projection | Read [bound status and offline reporting](references/status-and-offline.md). |

Read multiple references when a change crosses those boundaries. Inspect the
linked implementation and actual assertions before describing conformance.

## Preserve the lifecycle

1. Accept executable, workdir, environment policy, and argv separately. Prepare
   secure raw destinations and cancellation ownership before starting.
2. Persist stdout and stderr before bounded parsing or display. Keep raw bytes
   complete when downstream output, entry, or normalized-string budgets fill.
3. Aggregate by package, test, and occurrence. Keep metadata tied to source
   sequence and owning occurrence; keep malformed, missing, or contradictory
   semantic evidence explicit.
4. Drain and reap owned processes and streams. Finalize open state as
   incomplete; retain measured, estimated, and absent time distinctly.
5. Bind closed evidence files to authoritative status. Offline rendering must
   validate those bindings before it can claim success.
6. Preserve every contributing failure, then apply the documented exit
   precedence. Cancellation remains authoritative through report publication.

## Verification

Select owner tests for the changed boundary, app tests for composition, and
controlled CLI fixtures for end-to-end behavior. Native host tests are required
to qualify OS-specific signaling and reaping. Preserve raw output alongside
expected normalized state when reproducing malformed or fragmented records.
Do not turn a documentation-only correction into a process execution audit.

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

- Duplicate status members or mismatched evidence bindings cannot establish
  offline success, even when every parsed test passed.
- A policy-free no-coverage rerender removes only the coverage binding; a
  recorded coverage policy rejects that option before bundle mutation.
- Entry/string exhaustion causes explicit incomplete evidence. Ordinary report
  output clipping reports truncation without inventing a semantic failure.
