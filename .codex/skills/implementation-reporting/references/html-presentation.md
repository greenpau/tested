# HTML Presentation and Coverage Explorer

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

## Sort test-report summary tables

Default Slowest occurrences and Weighted coverage by file to Package ascending.
Keep slowest selection based on duration before sorting the HTML presentation;
console summaries retain their duration ranking. Verify limits of zero, one,
and several occurrences, including duration ties and repeated attempts across
packages. Package ordering must not change the selected occurrences or mutate
the source snapshot. Split coverage paths into Package and File only after
redacting the complete path, retaining the full redacted path as the file title.
Use `.` for paths without a directory.

Enhance headers with native sort buttons, direction indicators, one active
`aria-sort`, and a polite announcement. Toggle direction on repeated activation;
start a newly selected column ascending. Compare text with a fixed English,
case-insensitive natural collator. Compare durations as exact nanoseconds and
statement counts as exact integers; compare coverage ratios by exact cross
multiplication, never formatted percentages. Keep unavailable coverage last in
both directions and equal values in their initial visible order. Reuse row
nodes and keep every package cell visible, including repeated package names
and `.` for the module root. Never suppress repeated labels after sorting.
Keep the two tables independent and preserve sorting
through filter clearing, view switching, and printing. Keep numeric cells on
one line and contain horizontal scrolling within each table. Without JavaScript,
render static tables in lexical package order with no inactive sort buttons.

## Display module-relative package names

Use the optional module path supplied by app orchestration as HTML presentation
context; never infer it from the shortest observed package or a common prefix.
Show this full, redacted path as Base package in Run assessment, or unavailable
when absent. Shorten exact root matches to `.` and slash-delimited descendants
to relative names in package/build headings and both summary tables. Preserve
unrelated and similarly prefixed import paths. Retain full redacted identities
in tooltips and searchable data; allow filters to match displayed labels too.
Sort by visible labels. Missing identities must not become an invented root.

Match original identities before redaction can merge them, then derive labels
from complete redacted identities. Redact a coverage file's entire path before
splitting or shortening it, so separator-spanning rules cannot expose secrets.
If that redaction removes the directory entirely, show an unavailable package;
do not reinterpret the redacted basename as the module root or recover the
directory by redacting its original text separately.
Keep raw evidence, coverage association, occurrence hierarchy, JSON, JUnit,
and console identities unchanged. Retain deterministic rendering for a fixed
base-module context and the fully labeled no-script/print fallback. Inspect
[HTML projections](../../../../pkg/report/html.go) and
[module discovery](../../../../pkg/app/module.go).

## Present compact package rows

Render packages in one continuous bordered list with grey header rows, without
separate card gaps or per-package rounding. Keep the package name at the left;
group duration beside weighted coverage at the right, followed by status on
the same desktop row. Keep ordinary short headers at roughly 40px; allow long
names, narrow screens, and critical
package notes to wrap. Preserve cached/no-tests flags, failed-build context,
incomplete reasons, and duration provenance. Do not add empty body padding to
packages that contain only tests.

Calculate package coverage from validated file statement weights, matching
original profile directories to exact result package import paths before
redaction. Never average percentages, include subpackages in a parent total,
or guess unmapped paths from suffixes. Show unavailable for absent, empty,
unmapped, or invalid/overflowed groups; retain measured zero coverage and
duration as numeric values. Keep this presentation separate from coverage
policy and raw evidence. Inspect [package projections](../../../../pkg/report/view.go)
and [HTML tests](../../../../pkg/report/html_test.go).

Use native buttons with deterministic numeric content IDs, `aria-controls`,
`aria-expanded`, and package-name context for each package disclosure. Hide the
complete package body and tests together, preserving native details and subtest
collapse state. Start failed/incomplete package output and metadata expanded,
just as failed/incomplete test evidence starts expanded. Preserve a user's
subsequent details choices through both individual and global package toggles.
Provide Collapse/Expand all packages beside the section title;
if any package is expanded, collapse all, otherwise expand all. Apply this to
filtered-out packages too. Keep package collapse independent of filters and
subtest disclosures; clear-all, sorting, and view changes preserve it.

Offer Name, Status, Coverage, and Duration in a labeled sort dropdown, defaulting
to Name ascending. Start new fields ascending and expose a keyboard-accessible
direction button with a polite sort announcement. Include the visible direction
in the button's accessible name along with the action. Use natural text, exact
nanoseconds/ratios, missing values last in either direction, and ascending name
then original order for ties. Move package nodes without rebuilding them. Keep
these controls absent when there are no packages, and hidden without JavaScript.
Print all package contents and restore screen state; print rules must hide
disclosure/sort controls even in the no-script fallback.

## Preserve test hierarchy across view changes

Keep ordinary collapsed test rows compact: name, duration, status, and an
Output/Details disclosure on one line when space permits, with approximately
40px or less height for short desktop names. Keep package identity in the
package heading and searchable data, without adding it to each test heading.
Place occurrence kind/ordinal, duration provenance, retained output, attributes,
and artifact paths inside one native details panel. Keep failed/incomplete
panels open initially and incomplete reasons/truncation notices outside the
collapsible panel. Show unavailable duration distinctly from measured zero.
Let long names wrap at narrow widths. Put a compact branch disclosure beside
the heading, with accessible action/count and parent context; preserve native
details keyboard behavior and the expanded no-script/print fallbacks.

Keep Flat as the initial test-report view and expose a keyboard-accessible
Flat/Nested radio group after the embedded script initializes. Resolve nested
edges from normalized `TestOccurrence.Parent` identities, including the exact
package and ordinal, before redaction can merge visible names. Use deterministic
numeric DOM identifiers. Do not reconstruct parents from displayed slash paths
or assume child and parent ordinals match. Retain missing or invalid parents at
the package root; require a strict ancestor-name boundary to prevent cycles.
Inspect [HTML hierarchy projection](../../../../pkg/report/html.go) and
[hierarchy tests](../../../../pkg/report/html_test.go).

Move the existing occurrence elements when switching layouts; preserve their
order, output, metadata, open details, filters, and branch-collapse state.
Keep indentation bounded for deep hierarchies. Expose separate labeled Package,
Test name, and Output search fields. Combine their case-insensitive literal
substring matches with Status using AND; ignore surrounding query whitespace.
Use redacted package/test identities, including build import paths and full
test names without attempt suffixes. Search retained output plus diagnostic
messages/previews, excluding presentation labels and metadata. Cache each row's
own output before nesting, and restrict package output to its package body so
descendants cannot create false matches. Treat absent package bodies as empty
output. Rows without a package or test
identity cannot directly match a nonempty filter for that field.

Reveal matching tests' packages and, in Nested view, ancestors as context.
Count direct matches independently of context rows and collapse state. Within
expanded packages, filters reveal matching branches and temporarily disable
subtest collapse controls; clearing filters restores the user's branch state.
Keep build and integrity
evidence filterable in both layouts. Clear all filters must reset all three
text fields, Status, and the changed-packages switch together without changing
the view, package sort and collapse state, saved branch state, or open details. Let the expanded mobile
toolbar scroll with the page so it does not obscure results on narrow screens.

Keep behavior offline and self-contained, with no storage or network access.
Without JavaScript, hide inactive controls and retain the flat report with
all native details initially open. WebKit cannot reliably reveal manually
closed details through print CSS alone: with scripts enabled, open details
for `beforeprint` and restore them after printing; without scripts, preserve
the expanded static fallback. Print rules must hide controls even when a
generic `[hidden]` override reveals filtered results.

## Navigate and filter by the shared source comparison

Provide a fixed relative `index.html` link at the top of the test report and
through the coverage page's progressive script. Build the coverage link with
DOM methods before validating the canonical explorer structure, so it remains
available when the explorer falls back. Do not alter Go-authored source bytes
or relax the removable head-injection contract. Test-report navigation works
without scripts; the canonical no-script coverage fallback remains unchanged.
Hide navigation in print and retain keyboard focus styling.

Offer Changed packages only using the exact comparison successfully published
with coverage HTML. Never invoke another Git comparison, infer a default
baseline, or embed source hunks/deleted text in the test report. Match current
profile file identities to exact original package directories before redaction;
include modified, added, renamed (even without hunks), and untracked files.
Exclude unchanged, unavailable, unknown, and non-profile identities. A changed
subpackage must not mark its parent; redaction collisions must not merge
membership. Count matching packages retained in the result.

Use the coverage switch's visual style and native checkbox/switch semantics.
Start off, hide without a valid explicit comparison, and disable at zero
matching packages. AND this filter with the existing fields/status, preserving
package collapse and open output while temporarily revealing nested branches.
Keep unscoped diagnostics/output inspectable. Clear-all resets it; printing
reveals all evidence, and the no-script fallback stays fully expanded. Full-run
counts and summary tables remain unchanged. Document package-level scope and
the exclusion of test-file-only edits; do not imply per-test impact analysis.
Inspect [change projection](../../../../pkg/report/html.go) and
[report orchestration](../../../../pkg/app/report.go).

## Preserve the canonical coverage presentation

Own the shared tokens and page-specific styles in `pkg/report`. For
`coverage.html`, preserve the selected Go toolchain's source, annotation spans,
file selector, and script byte-for-byte. Apply one deterministic, exactly
removable head injection containing viewport metadata, content-security policy,
embedded theme CSS and progressive interaction JavaScript. Permit a safely
encoded dynamic payload only for a bounded source comparison explicitly
selected by `--coverage-diff-base`; treat all payload strings as untrusted and
never cast them to trusted HTML or JavaScript. Bound and validate the
closing-head anchor, reject duplicate decoration in the head without
interpreting matching text in annotated source, stream into a separate secure
temporary file, and require removal of the exact rendered injection to restore
the Go-authored bytes. The selected Go template contract uses the
case-sensitive `</head>` anchor and a 1 MiB maximum head; changing either
requires compatibility evidence.

Keep the canonical Go selector and source panels as the no-JavaScript and
unsupported-template fallback. Initialize the explorer after DOM readiness,
validate the complete expected selector-to-panel mapping before mutation, and
drive file changes through the retained selector. Build selected-file views
lazily with explicit source-size, line, and annotation-run bounds. Split
multiline coverage spans without losing text or coverage classes. Derive
packages lexically from exact profile source identities; do not normalize or
merge untrusted names.

Separate coverage focus from source changes. Let coverage mode show all source
or uncovered regions with three lines of context and accessible expandable
gaps. Expose Changes, changes-only scope, unified layout, and split layout only
when a real baseline payload exists. Render old/deleted lines without coverage
claims and retain current coverage annotations only on current/right lines.
With that baseline, expose a changed-files switch that narrows both package and
file selectors to modified, added, renamed, and untracked current-profile
files. Exclude unchanged, unavailable, and unmapped files; count a pure rename
as changed even without changed lines. Hide the switch without a baseline,
disable it when no covered file changed, preserve eligible selections, and let
an explicit file hash reveal its target by clearing incompatible filters.
Use real labels, selects, tabs or radio controls, buttons, table headers, a
polite status region, non-color add/delete cues, contained horizontal
overflow, and print behavior that reveals selected content. Build untrusted
text with DOM text nodes and fixed attributes only; forbid HTML string sinks,
dynamic code, network access, storage, and source-path URLs.

Keep every report-owned `.html`, `.css`, and `.js` source in
`pkg/report/assets/` and include the inventory through one unexported
`embed.FS`. Parse HTML with `html/template.ParseFS`; include fixed CSS and
JavaScript as named static subtemplates rather than trusted-content casts.
Load and validate the immutable asset bundle during renderer construction and
return errors for missing, malformed, unresolved, or structurally invalid
assets instead of panicking. Validate the coverage script against closing
script syntax, external references, unresolved template actions, and duplicate
markers. The JavaScript authored by `go tool cover` remains preserved toolchain
output separate from tested's embedded progressive enhancer.

## Verification and acceptance

Compare [embedded assets](../../../../pkg/report/assets/) with
[asset tests](../../../../pkg/report/assets_test.go),
[coverage decoration tests](../../../../pkg/report/coverage_html_test.go), and
[report rendering tests](../../../../pkg/report/report_test.go).

- Remove the exact head injection from real selected-toolchain output and
  recover its original bytes, including selector, source panels, spans, and
  Go-authored script.
- Filter packages, find uncovered multiline spans, and expand gaps without
  duplicated text. Malformed or oversized canonical DOM retains the fallback.
- With a real baseline, inspect additions, deletions, renames, unchanged files,
  unified/split layouts, and expandable gaps. Changed-file filtering includes
  rename-only files, excludes indeterminate mappings, and yields to explicit
  hash navigation. Without a baseline, expose no source-change controls.
- Inspect 1440×1000 and 390×844 viewports for toolbar/source overlap, unintended
  page-width overflow, clipped controls, and unreadable metrics or artifacts.
  Verify light/dark appearance, keyboard focus, and print behavior separately.
- Printing a filtered test report reveals every row and expanded retained
  evidence while hiding controls. Print the selected coverage file and legend.

The repository's Go tests check generated markup, CSS, scripts, security, and
selected-toolchain decoration. They do not establish browser layout or actual
keyboard/print interactions. The [browser suite](../../../../scripts/browser/)
and `make e2e-browser` exercise generated `file://` test reports in Chromium and
WebKit, including compact-row height and expandable evidence, repeated and
redacted identities, natural/exact numeric table sorting, slowest selection
limits, independent and combined filters, field isolation, clear/reset state,
missing parents, incomplete/build-only/empty evidence, keyboard focus, print, mobile/light/dark
layouts, no-script fallback, and deterministic offline rerendering. Inspect its
screenshots as well as assertions. Local Git fixtures also qualify shared
change membership, coverage changed-file selection, and report-index navigation,
including coverage explorer fallback. Other coverage explorer interactions and
browser engines remain outside that suite's qualification. Keep previews disposable and report unverified modes
explicitly. Do not install browser tooling for a prose-only edit.
