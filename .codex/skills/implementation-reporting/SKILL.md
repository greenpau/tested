---
name: implementation-reporting
description: Implement or review tested live console and artifact reports, including compatibility test HTML, summary JSON, JUnit XML, index HTML, escaping, redaction, deterministic ordering, duration quality, secure permissions, atomic publication, and manifest metadata. Use when changing pkg/report, report-mode behavior, or report projections consumed by humans and CI.
---

# Implementation Reporting

Render every presentation from an immutable normalized result and coverage
snapshot. Keep report convenience downstream from execution evidence: a report
may explain a run, but it may not reinterpret child status or mutate raw
artifacts.

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

## Keep the HTML visual system coherent

Treat `index.html`, `test_output.html`, and `coverage.html` as three entry
points into one compact operational interface. The adopted BDS token contract
uses light background/card `oklch(1 0 0)`, foreground `oklch(0.145 0 0)`,
muted `oklch(0.97 0 0)`, muted foreground `oklch(0.556 0 0)`, border
`oklch(0.922 0 0)`, primary `oklch(0.5 0.134 242.749)`, and ring
`oklch(0.588 0.158 241.966)`. Its dark equivalents are background
`oklch(0.145 0 0)`, card `oklch(0.205 0 0)`, foreground
`oklch(0.985 0 0)`, muted `oklch(0.269 0 0)`, muted foreground
`oklch(0.708 0 0)`, primary and ring `oklch(0.685 0.169 237.323)`, and
translucent white borders.

- Use the `0.45rem` base radius, an Inter-first system font stack, and a
  monospaced exact-value stack.
- Prefer flat bordered sections, muted table headers, divided rows, small
  semantic status pills, and tabular metrics.
- Build primary spacing from `0.25rem`, `0.5rem`, `0.75rem`, `1rem`, and
  `1.5rem`; optical text and pill adjustments may use smaller intermediate
  values.
- Avoid decorative gradients, remote assets, shadows, and unnecessary nested
  cards;
- On report-owned markup, support semantic heading order and table scopes,
  narrow viewports, horizontal overflow for dense tables, visible keyboard
  focus, WCAG 2.2 AA text contrast, and system dark mode. The Go-authored
  coverage body is exempt from markup changes.
- Keep output deterministic and self-contained. Use
  `default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action
  'none'` for the index. Add `script-src 'unsafe-inline'` only to the test and
  coverage pages because their fixed embedded behavior requires it.
- In print, restore all test rows hidden by interactive filtering and expand
  their native details while hiding filter controls. Print the index catalog
  as rendered and the currently selected Go coverage file with its legend.

Own the shared tokens and page-specific styles in `pkg/report`. For
`coverage.html`, preserve the selected Go toolchain's source, annotation spans,
file selector, and script byte-for-byte. Apply only a fixed,
untrusted-input-free head injection containing viewport metadata,
content-security policy, and theme CSS. Bound and validate the closing-head
anchor, reject duplicate decoration in the head without interpreting matching
text in annotated source, stream into a separate secure temporary file, and
require removal of the exact injection to restore the Go-authored bytes.
The selected Go template contract uses the case-sensitive `</head>` anchor and
a 1 MiB maximum head; changing either requires compatibility evidence.

Keep every report-owned `.html`, `.css`, and `.js` source in
`pkg/report/assets/` and include the inventory through one unexported
`embed.FS`. Parse HTML with `html/template.ParseFS`; include fixed CSS and
JavaScript as named static subtemplates rather than trusted-content casts.
Load and validate the immutable asset bundle during renderer construction and
return errors for missing, malformed, unresolved, or structurally invalid
assets instead of panicking. The JavaScript authored by `go tool cover` is
preserved toolchain output, not a tested-owned source asset.

## Render live output

Support `plain`, `markdown`, and `json` output formats as projections of the
same semantic updates and final snapshot. Apply `--quiet`, `--color`, and
`--slowest` as presentation choices rather than changes to aggregation.

- Make plain output safe for a terminal. Neutralize untrusted control
  sequences unless color originates from the trusted renderer and color is
  enabled.
- Escape Markdown metacharacters and structural boundaries so child output
  cannot create deceptive headings, links, or code blocks.
- Emit valid complete JSON values or documented JSON records. Never mix prose
  or ANSI bytes into JSON mode.
- Keep live output useful under partial input, but label provisional state and
  replace it only with evidence-backed terminal state.
- Sort final summaries and slowest lists with deterministic tie-breakers that
  include package, test name, and occurrence ordinal.

## Escape and redact

Compile every configured `--redact` regular expression during CLI, renderer,
and console option validation. Accept at most 32 expressions, 4096 bytes per
expression, and 32 KiB in aggregate. Reject expressions that match empty input.
Then apply the ordered rules to untrusted presentation fields before
format-specific escaping. A contextual zero-width match or projected
replacement growth beyond the larger of the original byte length and the
fixed omission marker must replace the whole value with that marker and stop
later rules. Evaluate the growth bound before accepting expanded output; do
not let repeated rules reprocess an omission marker or amplify a presentation
value. Apply rules consistently to console, test HTML, summary JSON, JUnit XML,
index HTML, and report diagnostics. Do not apply them to live-captured/imported
event, stderr, or coverage evidence, `coverage.html` source, `run.json`, or
manifest-integrity fields; protect all managed evidence with `0600` and mark it
sensitive in the manifest.

Escape for the destination:

- use contextual `html/template` escaping and avoid untrusted `template.HTML`;
- use `encoding/json` rather than manual quoting;
- use `encoding/xml`, remove or replace XML-forbidden control characters, and
  prevent attribute/text confusion;
- escape Markdown and terminal control syntax independently;
- generate links from fixed managed artifact names, not child-provided URLs or
  paths.

Test secrets next to punctuation, Unicode, HTML entities, JSON/XML escapes, and
ANSI bytes. Ensure a presentation cannot reveal a redacted value through a
title, diagnostic, package name, test name, output body, build failure, path,
tooltip, link, or metadata attribute. Include hostile repeated-dot rules,
contextual zero-width assertions, maximum configuration bounds, and fuzzed
values; assert that output stays within the redaction growth ceiling.

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

## Make outputs deterministic

For the same normalized snapshot, coverage snapshot, renderer version, title,
and redaction configuration:

- sort packages, occurrences, diagnostics, coverage files, and manifest entries
  by documented stable keys;
- derive stable identifiers from semantic identities rather than map order,
  wall clock, random values, or temporary paths;
- use fixed schema and renderer version fields;
- serialize with stable whitespace, newline, numeric, and percentage rules;
- omit volatile host paths and generation timestamps unless they are captured
  source evidence required by the schema.

Preserve event source order inside one occurrence where order conveys evidence.
Determinism does not authorize sorting a diagnostic transcript into a different
story.

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

## Acceptance scenarios

- Hostile child text is inert in terminal, Markdown, HTML, JSON, and XML.
- A configured secret is absent from every presentation but remains exact in
  secure raw evidence.
- Repeated test names produce independent HTML sections and JUnit cases with
  stable occurrence identities.
- Incomplete tests appear as indeterminate/errors, and build-only failures
  remain prominent.
- Measured, estimated, and absent durations stay distinguishable.
- Equivalent snapshots render byte-identical derivatives and manifest
  inventories.
- A failure during one derivative leaves raw evidence intact and no manifest
  claiming the failed file.
- Real selected-toolchain coverage HTML retains `#files`, `pre.file`, coverage
  spans, and its change script after decoration; removing the exact fixed head
  injection restores the original bytes.
- Headless browser inspection at 1440×1000 and 390×844 shows no toolbar/source
  overlap, unintended page-width overflow, clipped controls, or unreadable
  metric and artifact layouts in light mode. CSS and renderer tests cover the
  system-dark, visible-focus, and print contracts without external resources.
- Printing a filtered test report reveals every filterable row and expanded
  retained evidence; controls do not consume print space.
