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
keyboard/print interactions. Use available browser tooling when qualifying
those changes, keep previews disposable, and report unverified modes explicitly.
Do not invent a browser test target or install tooling for a prose-only edit.
