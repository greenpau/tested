# tested User Guide

## What tested runs

`tested run` invokes the selected Go executable directly, without a shell:

```text
go test -json -coverprofile=<absolute-output-path> <your arguments>
```

The default arguments are `./...`. `tested` owns `-json` and `-coverprofile`;
supplying either yourself is rejected. Everything after `--` is preserved as a
Go test argument vector, including package patterns, `-run`, `-race`, `-count`,
`-coverpkg`, build tags, and arguments following Go's own `-args` delimiter.

These commands are equivalent ways to select packages and test flags:

```bash
tested run -- ./...
tested run -- -run TestLogin ./...
tested run ./... -run TestLogin
```

The explicit `--` form is recommended because it makes the boundary between
tested options and Go options unambiguous.

## Command reference

### Run

```text
tested [run] [options] [-- go-test-arguments...]
```

Important options:

| Option | Default | Effect |
| --- | --- | --- |
| `-C`, `--work-dir` | `.` | Project directory for Go execution and source lookup |
| `-o`, `--output-dir` | `.coverage` | Artifact directory, relative to the project |
| `--go` | `go` | Go executable or toolchain selector |
| `--title` | project directory | Human-readable report title |
| `--no-coverage` | disabled | Produce test reports without a cover profile; incompatible with status that records a coverage policy |
| `--minimum-coverage` | disabled | Exact required weighted statement percentage |
| `--coverage-diff-base` | disabled | Compare coverage source with an explicitly selected local Git commit |
| `--format` | `plain` | Console projection: `plain`, `markdown`, or `json` |
| `--color` | `auto` | ANSI color policy: `auto`, `always`, or `never` |
| `--quiet` | disabled | Suppress all live progress, logs, and heartbeats |
| `--slowest` | `10` | Number of slow occurrences in the final summary |
| `--max-event-bytes` | 16 MiB | Largest JSON record and aggregate fragmented benchmark line assembled in memory; `0` disables the byte limit |
| `--max-test-output-bytes` | 1 MiB | Output retained per normalized scope; `0` is unlimited |
| `--max-total-output-bytes` | 64 MiB | Aggregate output retained across all normalized scopes; `0` is unlimited |
| `--max-result-entries` | 1,000,000 | Aggregate normalized entities and retained chunks; `0` is unlimited |
| `--max-normalized-bytes` | 128 MiB | Aggregate retained identity and metadata strings; `0` is unlimited |
| `--redact` | none | Ordered Go regular expression replaced in derived reports; at most 32 rules, 4096 bytes each and 32 KiB total; each rule must not match empty input |

All byte-limit flags accept `0` as an explicit unlimited setting. Every
nonzero value must be at least 1024 bytes. The per-scope output limit applies
independently to each test occurrence, package, build, and unattributed output
scope. The total-output budget limits their combined retained prefixes, while
the result-entry budget limits aggregate semantic cardinality and retained
output chunks. The normalized-byte budget independently counts package, test,
build, attribute, artifact, unknown-action, and run-metadata strings; output
text is counted by the output budgets instead. Exhausting either semantic
budget marks the result incomplete. Output clipping remains a presentation
fact because the raw JSONL stays complete. None of these limits changes raw
evidence.

The event limit never truncates `test_output.jsonl`. If a record is too large
to decode, tested drains and records it, marks report integrity partial, and
continues framing subsequent records. Within a decoded record, at most 64
fields may be unknown to the selected test/build schema, and their decoded
names plus exact retained JSON values may total at most 64 KiB. Duplicate
decoded member names, including escaped-equivalent names and duplicates inside
retained extension objects, are malformed. These failures affect only the
semantic projection; the managed event log remains byte-faithful.

#### Exact coverage thresholds

`--minimum-coverage` accepts at most 256 bytes and only the ASCII plain-decimal
grammar `DIGIT+("."DIGIT+)?` from `0` through `100`, inclusive. Values such as
`082.5000` are canonicalized to `82.5`. Leading signs, surrounding whitespace,
exponents, percent suffixes, digit separators, NaN, and infinities are
rejected.

The decision compares the exact decimal to
`100 * covered statements / total statements` with overflow-safe integer
arithmetic. Exact equality satisfies the policy. A rounded percentage shown in
a terminal or report never decides the result. When the child run is otherwise
successful and coherent, below-threshold coverage exits 3; missing, empty, or
invalid coverage is an infrastructure/reporting failure and exits 2. A child
failure or cancellation retains its higher-precedence status.

#### Coverage source comparisons

`--coverage-diff-base REV` enables the change-focused and side-by-side views
in `coverage.html`. The revision is passed to Git as one argument and must
resolve to exactly one commit in the repository selected by `--work-dir`.
`tested` does not infer a default branch, contact a remote, or fetch a missing
object. This opt-in mode requires the local `git` executable; ordinary coverage
generation does not. Both `run` and `report` accept the option; report mode
resolves the commit in its selected work directory when it regenerates the
page. An empty value leaves source comparison disabled.

The comparison uses source from the selected commit as its baseline and the
source used by the current coverage report as its destination. Because the
HTML is self-contained, it can embed baseline lines that were deleted from a
modified or renamed current file. Baseline and current source can contain
credentials, private paths, or implementation details; both are unredacted
coverage evidence and must be protected accordingly.
`--coverage-diff-base` cannot be combined with `--no-coverage`.
If a live or offline bundle has no usable coverage profile, an explicitly
requested comparison fails report generation instead of being silently
ignored.

### Offline report

```text
tested report [options] [event-log]
```

Without explicit paths, report mode reads:

```text
<output-dir>/test_output.jsonl
<output-dir>/coverage.out
<output-dir>/run.json
```

Use `--events`, `--stderr`, `--coverprofile`, and `--run-metadata` for archived
evidence. External event, stderr, and profile bytes are copied into
`test_output.jsonl`, `stderr.log`, and `coverage.out` under the selected managed
output directory before analysis. An external valid `run.json` is decoded and
written in tested's deterministic canonical form as managed `run.json`; a
status file already at that exact managed path is normally left byte-for-byte
unchanged.

When an explicitly selected external `run.json` binds `stderr.log` or
`coverage.out` and no explicit companion path overrides it, tested imports the
same-named sibling from the status file's directory. A custom event selection
never consumes an unrelated default status file. Managed stderr, profile, or
status artifacts left from an older bundle are removed when the selected
events/status do not bind or select them. Unknown files in the output directory
are never removed. A source already at its exact managed destination is not
rewritten or deleted merely because validation fails; the command instead
reports incomplete/infrastructure status and withholds the manifest.

A successful `report --no-coverage` removes both `coverage.out` and
`coverage.html`. When valid policy-free status binds `coverage.out`, tested
canonically rewrites only the managed `run.json` projection without that
binding; child exit, signal, cancellation, issues, capture state, and retained
event/stderr bindings remain authoritative. Repeating the command preserves
that projected status and produces the same deterministic manifest. If the
selected status records a coverage policy, tested rejects `--no-coverage`
before artifact preparation and leaves the existing bundle unchanged.
Removing the profile or policy would otherwise erase the evidence for an
authoritative coverage decision.

`run.json` contains the authoritative child status and size/SHA-256 bindings to
the evidence from that run. A bare event log can still be imported and rendered
for inspection, but its child exit is unknown, its outcome is incomplete,
report mode exits 2, and no manifest is published.

Coverage generation still runs with the project directory as its working
directory so module import paths in a profile resolve correctly. When trusted
run metadata has no coverage binding, report mode infers that no profile is
available; `--no-coverage` need not be repeated.

Offline reports prefer Go's event `Elapsed` value as measured duration and use
event timestamps only as an estimated fallback. Live runs additionally retain
monotonic process wall time. `--allow-failures` lets report mode return success
after rendering bound, known failing test evidence; malformed, unbound, or
incomplete evidence is still an error.

#### `run.json` v1 integrity contract

Status metadata uses schema `tested/run/v1` and is limited to 1 MiB. The decoder
accepts exactly one JSON value, rejects duplicate members at every depth,
unknown fields, and trailing values, and validates cross-field state before
trusting it. Signal cancellation accepts only `interrupt` with recommended
exit 130 or `terminated` with recommended exit 143. The document records:

- child argv and work directory when known;
- wall-clock start/finish evidence and optional nonnegative `duration_ns`,
  measured from the runner's monotonic child span and permitted only when the
  child started;
- child-start, exit, signal, cancellation, recommended-exit, and
  capture-completeness state;
- bounded run issues and the exact coverage-policy decision when requested;
- one required `test_output.jsonl` binding and optional `stderr.log` and
  `coverage.out` bindings. No other binding name is accepted, so there are at
  most three; each has a unique canonical name, nonnegative size, and lowercase
  64-digit SHA-256. Encoding sorts bindings lexically by name.

Offline reporting verifies every retained managed evidence file against these
bindings. A bound file must exist, and an existing evidence companion must be
bound. The hashes detect accidental mixing or alteration; they do not
authenticate a bundle against another process that can rewrite both evidence
and metadata.

On POSIX systems, tested also requires every retained managed regular file to
have one filesystem link before changing its mode or computing a managed
digest. A multiply-linked managed file is rejected so an external alias cannot
be chmodded or admitted to a manifest. Explicit external imports may themselves
be hard links: tested reads them as sources and atomically publishes a new,
independent managed file without changing source contents or permissions.

### Version and help

```bash
tested version
tested help
```

Release builds and `make build` / `make install` include the semantic version,
Git branch and full commit, build user, UTC build time, and Go runtime version.
`-dirty` on the commit identifies a checkout with uncommitted changes, including
untracked files. `detached` identifies a checkout without an active branch.

`make install` installs into Go's configured `GOBIN`, falling back to the first
`GOPATH` entry's `bin` directory. For example, from a Git checkout:

```bash
GOBIN="$HOME/dev/bin" make install
"$HOME/dev/bin/tested" version
```

Plain Go builds use Go's embedded module version and VCS metadata when present.
Explicit linker stamps take precedence. Go does not record the branch, build
timestamp, or builder by default; `go install ...@version` also usually omits
the commit. Missing values are shown as `not recorded`, with a suggestion to
use `make install`. Any embedded VCS timestamp is labeled **commit time**,
separately from **built**. Version reporting never queries the current working
directory, current user, or executable modification time to guess provenance.

## Artifact lifecycle

The output directory is a dedicated private report root. At the start of a live
run, tested invalidates only its known managed artifact names. It never
recursively clears an arbitrary user-selected directory and never removes
unknown files.

Live-captured or imported evidence is handled separately from derivatives:

1. Create secure live evidence files, or copy selected imports into canonical
   managed evidence names.
2. For a live run, execute Go tests and stream standard output to
   `test_output.jsonl` before parsing it.
3. Finalize interrupted or nonterminal occurrences as incomplete.
4. Parse the coverage profile when one exists.
5. Persist `run.json` with child outcome and cryptographic bindings to the
   closed raw evidence.
6. Atomically publish test HTML, summary JSON, JUnit XML, index HTML, and
   Go-authored coverage HTML with tested's fixed presentation layer.
7. Hash the published set and write `manifest.json` last.

A failed test or build still receives reports whenever enough evidence exists.
A missing profile after a failed build is reported as unavailable, never
replaced by a fabricated empty profile.

Artifact presence is conditional. Both coverage artifacts are absent when
coverage is disabled. `coverage.html` requires a valid profile; a nonempty
invalid `coverage.out` may remain as diagnostic evidence without HTML.
Imported bundles may omit unbound stderr or coverage evidence. `index.html`
links only to managed files that exist. `manifest.json` is withheld or removed
after cancellation, malformed or unbound evidence, incomplete capture,
contradictory status, or any requested report publication failure. An
authoritative ordinary test/build failure or exact coverage-gate failure can
still be a coherent generation with a manifest.

## Cancellation and exit precedence

The CLI listens for SIGINT and SIGTERM throughout both `run` and `report`.
During a live run, tested signals the owned process tree, continues draining
stdout and stderr, waits up to the default two-second graceful interval, then
forces termination and performs a bounded reap/pipe-drain cleanup. Unix uses an
owned process group; Windows uses a kill-on-close Job Object where host policy
allows it and a bounded compatibility fallback otherwise.

Cancellation also covers coverage HTML and derived report publication. A
cancelled report run removes or withholds `manifest.json` so already-written
individual files cannot be mistaken for a coherent generation.

Final exit precedence is:

1. cancellation (`130` for SIGINT, deadline, or programmatic cancellation;
   `143` for SIGTERM);
2. an authoritative nonzero child exit, preserved exactly, including the
   conventional `128+signal` projection on Unix;
3. execution, capture, event/result integrity, or incomplete evidence (`2`);
4. unallowed failing event evidence (`1`), except that a zero child exit
   conflicting with failing events is an integrity error (`2`);
5. an exact unmet minimum coverage policy (`3`);
6. report rendering/publication failure (`2`);
7. success (`0`).

Ordinary Go test/build failure is normally child exit 1. In offline mode,
`--allow-failures` can convert only coherent, bound exit-1 test evidence to
success; it never permits cancellation, infrastructure damage, corruption, or
incomplete evidence.

## Reading the console summary

Plain and Markdown output stream package and test lifecycle events as they
arrive, including subtests, repeated occurrences, pause/resume, and completion.
Package headings establish context for the following test and log lines, so
each line shows the test name without repeating its package path. Context is
shown again when output switches packages or resumes after a stage, stderr,
or unattributed output.
Package completion lines include elapsed time and occurrence counts. Test and
build log text and child stderr appear with scope prefixes; terminal controls
and Markdown syntax are escaped. Go controls when it emits events and logs:
while it is silent, tested reports the current stage, elapsed time, and number
of event records after roughly 10 seconds without displayed progress. The
heartbeat is checked once per second and repeats during continued inactivity.

Stages also cover artifact preparation, evidence finalization and verification,
coverage processing, optional Git comparison, each report publication, and
manifest hashing. Offline `tested report` shows its own processing stages and
record count, without replaying historical tests as live execution.

Live details have a separate 4 MiB budget. Each displayed line is bounded to
4 KiB; log previews inspect at most 16 KiB per event or stderr read and bound
formatted expansion. A notice marks omitted detail. Stage messages, periodic
status, and the final summary continue after the detail budget fills. The
configured result-retention limits also apply to event log previews. Raw
`test_output.jsonl` and `stderr.log` remain complete; live prefixes, wrapping,
and truncation never change those files.

Use `--quiet` to keep only the final summary. `--format json` continues to emit
exactly one final JSON summary, with no interleaved progress or log text.
When `--redact` is configured, live log payloads are omitted with a notice:
arbitrary regular expressions can span event or read boundaries, so redacting
individual fragments would risk exposing partial secrets. Lifecycle identities
and stage messages still use configured redaction; completed test/build report
transcripts use the normal redaction rules.

The final summary reports child exit, cancellation cause and projected shell
exit, run-level integrity issues, failures, weighted global coverage, and the
slow-test list in deterministic order. A console write failure is a report
failure: raw capture continues, the child status remains authoritative, and no
manifest claims a complete presentation.

The global percentage comes from `coverage.out`:

```text
covered statements / total statements
```

A block counts as covered when its execution count is greater than zero in
`set`, `count`, or `atomic` mode. File or package percentages are not averaged.

Repeated names are displayed with an occurrence number:

```text
TestRefreshToken
TestRefreshToken [attempt 2]
TestRefreshToken [attempt 3]
```

This preserves `go test -count=N`, multiple `-cpu` runs, and repeated
benchmarks.

## HTML and CI reports

`test_output.html` is self-contained: CSS, script, normalized results, and
bounded output are embedded locally. It provides:

- separate package, test name, and output filters, plus **Clear all filters**;
- direct status filtering for failed and incomplete results;
- a Flat / Nested view toggle, with expandable subtest branches;
- repeated-occurrence labels;
- package build and setup diagnostics;
- explicit incomplete states;
- duration source and weighted coverage;
- output truncation notices pointing to the raw JSONL.

Packages appear in one continuous list with grey headers. Each header shows
the package name, with duration beside weighted coverage and status at the right.
HTML package names are relative to the module declared in the nearest `go.mod`
in or above the selected working directory. For example, `github.com/greenpau/tested`
becomes `.`, and `github.com/greenpau/tested/internal/tag` becomes `internal/tag`.
**Run assessment → Base package** shows the full module name. Full package
paths remain available on hover and in the Package filter. Packages outside
that module keep their full names; if the module cannot be read, full names
remain and Base package shows **unavailable**.
The arrow in a header hides or shows that package's output, metadata, and tests.
**Collapse all packages** beside **Packages and test occurrences** reduces the
list to package headers; **Expand all packages** restores the contents.
These controls affect all packages, including those hidden by filters, and
preserve open output panels and collapsed subtest branches. Filters do not
automatically reopen a package you collapsed.

**Sort packages** offers **Name**, **Status**, **Coverage**, and **Duration**.
Name ascending is the default. Selecting a field starts ascending; use the
direction button to reverse it. Names and statuses sort alphanumerically;
coverage and duration use exact numeric values. Unavailable values stay last
in both directions. Sorting preserves filters and disclosure state.
Package coverage sums statement weights from matching files without including
subpackages. Missing or zero-statement coverage shows **unavailable**; imported
profile paths that cannot be matched exactly to a package also remain unavailable.

Collapsed test rows show the test name, duration, status, and **Output** control
on one compact line when space permits. Expand **Output** (or **Details** for
tests without logs) to inspect occurrence details, duration source, attributes,
and artifact paths. Failed and incomplete panels start open; incomplete reasons
and truncation notices remain visible. Package names appear at the group level.

**Slowest occurrences** and **Weighted coverage by file** start sorted by
**Package**, ascending. Click any column heading to sort it; click again to
reverse the order. Text sorts alphanumerically (`Test2` before `Test10`), while
durations, statement counts, and percentages sort by their exact numeric
values. Unavailable coverage stays last. Coverage has separate **Package** and
**File** columns, and both tables show the package name in every row,
including `.` for the module root. The slowest table still selects the longest durations using `--slowest`;
choose **Duration** to order those rows by time. Each table keeps its sort when
you clear filters, switch views, or print.

Use **Package**, **Test name**, and **Output** together to narrow results.
Each field matches literal text without regard to case and ignores leading
and trailing spaces. Results must match every filled field and the selected
**Status**. Package matches package names, including build import paths;
Test name matches full test/subtest names across repeated attempts; Output
searches retained logs and diagnostic messages/previews. Filtering uses the
redacted text shown in the report. **Clear all filters** empties all three
fields, resets Status to **All statuses**, and turns off the change filter,
preserving your chosen view,
package sorting, collapsed packages/branches, and open output panels.

With `--coverage-diff-base REV`, **Changed packages only** shows all tests in
packages with modified, added, renamed, or untracked covered source files,
using the same comparison as Coverage source. The badge counts matching
packages in this report. This is a package-level view: edits only to test files
are outside the coverage source comparison. The switch combines with the
other filters, starts off, and is disabled when no reported package matches.
It is absent without a successfully generated comparison. Filtering preserves
package/output/branch state; run-wide diagnostics remain inspectable. Run totals
and summary tables still describe the complete run, and printing reveals all
tests.

Use **View → Nested** to group subtests beneath their recorded parent
occurrence. Repeated runs remain separate, including when a child runs only
on some attempts. **Expand all**, **Collapse all**, and the subtest buttons
control the branches. Text and status filters temporarily reveal matching
branches and their ancestors; clearing the filters restores your collapsed
branches. Switching views preserves filters and open output panels. Flat is
the default on each page load. Without JavaScript, the report keeps the flat
layout and starts all output panels expanded so they remain printable.
With JavaScript enabled, printing includes all retained results and output,
even when filtered or collapsed on screen. The toggle works when opening the
file directly.

`index.html`, `test_output.html`, and `coverage.html` share a compact
operational theme with no remote dependencies. **Report index** at the top of
the test report and enhanced Coverage source page returns to `index.html`.
The test-report link also works without JavaScript; coverage navigation is part
of its progressive enhancement. The pages respond to narrow
viewports, honor the system dark-mode preference, use monospaced text for exact
diagnostic values, expose visible keyboard focus, and provide print styles.
All report-owned HTML, CSS, and JavaScript sources are compiled into the
`tested` binary with Go's `embed` package; report generation does not depend on
templates or assets beside the executable.

`coverage.html` retains the document produced by the selected toolchain's
`go tool cover`, including its source annotation, coverage spans, file
selector, and script. Before atomic publication, tested streams that document
through a decorator which inserts one exactly removable viewport,
content-security-policy, embedded-style, and embedded-script layer before the
unique closing `head`. The progressive explorer filters packages, focuses
uncovered regions, and expands hidden context while preserving the canonical
Go page as its fallback. An explicit `--coverage-diff-base` adds a bounded,
contextually escaped edit model for unified and split source changes. Its
changed-files switch limits both selectors to packages and covered files that
are modified, added, renamed, or untracked. Tested does not redact, reorder, or
independently replace the Go-authored source report.

Coverage profile parsing has independent safety ceilings of 256 MiB of raw
profile data, 100,000 unique source files, and 1,000,000 unique normalized
blocks. Duplicate file names and coordinate blocks consume one cardinality
slot, while their bytes still count toward the profile limit.

Go 1.25 `attr` and Go 1.26 `artifacts` TestEvents are decoded as known events,
including their `Key`/`Value` or `Path` fields, and remain byte-faithful in
`test_output.jsonl`. Their source sequence and package/test occurrence are
preserved in the normalized result. `summary.json`, `test_output.html`, and
`junit.xml` project the normalized metadata after presentation redaction.
Artifact paths are informational text only: `tested` never turns a child
path into an HTML link.

`summary.json` is the compact agent/CI interface. It excludes ordinary passing
logs and includes bounded failure evidence, counts, timings, coverage, the
exact coverage-policy decision, child outcome, run assessment, and integrity
diagnostics. Artifact availability, sizes, and hashes are recorded separately
in `manifest.json`.

`junit.xml` exists for CI systems that ingest JUnit test cases. Repeated
occurrences receive distinct names. Synthetic execution and policy cases keep
an empty event stream with a failed child, cancellation, infrastructure
failure, and an unmet coverage gate from appearing green.

## Redaction and sensitive reports

Raw evidence is intentionally immutable. For example:

```bash
tested run \
  --redact '(?i)authorization:[[:space:]]+[^[:space:]]+' \
  --redact '(?i)(password|token|secret)=[^[:space:]]+' \
  -- ./...
```

The expressions are applied in order and replace complete matches throughout
plain/Markdown/JSON console presentations, `test_output.html`, `summary.json`,
`junit.xml`, and `index.html`, including titles, identities, paths,
diagnostics, metadata, and retained output. They do not alter:

- `test_output.jsonl`;
- `stderr.log`;
- `coverage.out`;
- current and optional baseline source embedded by `coverage.html`;
- `run.json`;
- `manifest.json` names, sizes, or digests.

`tested` rejects an expression that matches empty input and bounds the
configuration to 32 expressions, 4096 bytes per expression, and 32 KiB in
total. Some assertions, such as a word boundary, match zero bytes only when
surrounding text provides context. If such a contextual zero-width match is
encountered, or if a replacement would exceed the larger of the original
value's byte length and the fixed omission marker, the whole value becomes
`[REDACTED: value omitted]`. Processing stops for that value, so later ordered
rules cannot expand the marker. The bound is checked before accepting each
replacement; redaction work and retained redacted output remain linear in the
input and configured limits.

When a derived output prefix was truncated before a configured expression
could be evaluated safely, tested redacts that retained output conservatively
instead of exposing a possible partial secret. The complete raw evidence
remains sensitive.

If source or raw output is too sensitive for shared storage, do not upload
those files. Filesystem modes are a local protection, not a substitute for CI
artifact access control.

## CI examples

Basic:

```yaml
- name: Test
  run: tested run -- -count=1 ./...
```

With a coverage gate:

```yaml
- name: Test with coverage policy
  run: tested run --minimum-coverage 80 -- -race -count=1 ./...
```

Upload the complete directory only when its sensitivity is understood:

```yaml
- uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
  with:
    name: test-reports
    path: .coverage/
```

The manifest is the conditional coherence marker. Consumers should not assume
a report set is complete when the manifest is absent or the summary reports
partial integrity.

## Troubleshooting

### Go command cannot start

Check `--go`, `PATH`, and `--work-dir`. `stderr.log` contains only bytes written
by a successfully started child. Startup failures appear in the terminal,
`run.json` issues, `summary.json`, and JUnit when the artifact root could be
created. The process exits with code 2.

### Tests failed but there is no coverage HTML

Go writes a cover profile only when it reaches the applicable test completion
path. A compile, setup, or early toolchain failure may leave no valid profile.
The test report and stderr evidence remain authoritative.

### Report mode cannot resolve coverage source

Run report mode with `-C` pointing at the module or workspace used to produce
the profile:

```bash
tested report -C /path/to/project --coverprofile /archive/coverage.out
```

For a custom event stream, provide its matching status evidence too:

```bash
tested report -C /path/to/project \
  --events /archive/test_output.jsonl \
  --coverprofile /archive/coverage.out \
  --run-metadata /archive/run.json
```

The event and profile must match the bindings in `run.json`. tested rejects a
mixed or stale bundle and does not publish `manifest.json`.

### A test is incomplete

An occurrence started but did not receive `pass`, `fail`, `skip`, or `bench`.
Typical causes are interruption, a killed process, corrupt event framing, a
panic that ended the stream unexpectedly, or archived evidence that was
truncated. tested never guesses that such an occurrence passed.

### JSON record exceeds the event limit

Increase `--max-event-bytes` to at least 1024 or use `0` to disable the byte
limit for decoding and fragmented benchmark-line assembly. The managed
event-log bytes were still preserved. A fixed benchmark fragment-count safety
limit remains in effect; treat unlimited byte decoding as a memory-trust
decision.
