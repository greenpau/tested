---
name: implementation-reporting
description: Implement and review tested presentations and secure artifact publication. Use when changing console output, HTML assets, JSON or JUnit projections, escaping, redaction, managed file protections, or manifests.
---

# Implementation Reporting

Own projections of immutable result/coverage snapshots and their secure
publication. Keep protocol interpretation, profile math, and child-status
policy with their owners. A report explains evidence; it cannot rewrite raw
bytes or turn incomplete execution into success.

## Select the report boundary

| Task | Required detail |
| --- | --- |
| Compatibility inventory, evidence imports, permissions, atomic writes or manifest coherence | Read [artifacts and publication](references/artifacts-and-publication.md). |
| Shared design, embedded assets, coverage decoration or interactive explorer | Read [HTML presentation](references/html-presentation.md). |
| Console formats, escaping, redaction limits or deterministic serialization | Read [presentation security](references/presentation-security.md). |
| Occurrence identity, metadata, duration quality, budgets, outcomes or JUnit sentinels | Read [outcome projections](references/outcomes-and-metadata.md). |

Load multiple references for changes spanning formats or storage. Inspect
[renderer composition](../../../pkg/report/renderer.go), the relevant source,
and assertions linked from each reference before changing behavior.

## Render and publish

1. Consume the normalized snapshot and presentation options, preserving
   occurrence ordinals, source sequence, explicit incomplete state, duration
   quality, and retention facts.
2. Redact untrusted presentation values before destination-specific escaping.
   Keep child artifact paths non-clickable. Keep evidence, status bindings,
   coverage source, and manifest integrity fields exact and access-limited.
3. Keep all report-owned HTML/CSS/JavaScript in `pkg/report/assets/`, embedded
   at compile time. Return asset-construction errors and preserve the canonical
   Go coverage fallback and reversible injection.
4. Stage complete derivatives securely, publish atomically, and hash final
   bytes. Link only existing managed artifacts and publish the conditional
   manifest last when the generation is coherent.
5. On failure, retain evidence and child status, withhold incomplete derivatives
   from the manifest, and fail an otherwise successful requested report.

## Verification and acceptance

Use renderer/security tests and stable fixtures for byte-level behavior,
artifact tests for storage, and app tests for end-to-end status and bundle
composition. Real browser inspection qualifies interactive/layout changes;
string assertions alone do not. Record which evidence was actually observed.

- Hostile text stays inert in every output format, and configured secrets are
  absent from presentations while secure raw evidence remains byte-faithful.
- Repeated names stay separate in HTML, JSON, JUnit, and slowest lists.
- Incomplete, build-only, unknown-child, cancelled, and policy-failed runs
  cannot yield a false-green CI suite. Missing time stays distinct from zero.
- Equivalent snapshots/options render identical bytes and manifest inventories.
- Failed publication preserves evidence and leaves no manifest claiming a
  missing or partial derivative. Coherent test failure can still have a valid
  diagnostic bundle.
- Coverage HTML keeps canonical Go bytes; browser enhancement fails back safely
  and only exposes source changes with an explicitly resolved baseline.
