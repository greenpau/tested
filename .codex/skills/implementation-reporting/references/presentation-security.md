# Live Output, Escaping, and Redaction

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

## Format coverage percentages

Render weighted coverage totals, per-file coverage, and actual policy coverage
in HTML, plain/Markdown console output, and JUnit messages with two decimal
places, rounding half up from the exact integer ratio. When positive coverage
would round to `0.00%`, show `<0.01%`; when partial coverage would round to
`100.00%`, show `>99.99%`. Keep truly zero/full coverage and unavailable evidence
distinct. Redact formatted policy text before destination-specific escaping.

Keep the requested minimum exact and preserve the supplied satisfaction
decision even when rounded actual coverage appears equal to that minimum.
Keep JSON numeric percentages, decimal `percent_exact` fields, and the
high-precision policy `actual` independent of concise display strings. Preserve
the existing JSON precision and durable run metadata used for offline
validation. Inspect [display formatting](../../../../pkg/report/view.go),
[policy views](../../../../pkg/report/assessment.go), and
[JSON projections](../../../../pkg/report/summary.go) together when changing
these boundaries.

## Verification entrypoints

Inspect [console tests](../../../../pkg/report/console_test.go),
[redaction tests](../../../../pkg/report/redaction_test.go),
[format security tests](../../../../pkg/report/report_test.go), and
[fuzz inputs](../../../../pkg/report/fuzz_test.go). Check the resulting
presentation as well as its source model: escaping, redaction, and stable
ordering must survive the complete renderer boundary.
