# Tested Authoring Supplement

## Scope and documentation ownership

Keep adopted guidance and metadata inside this checkout. Read external or
sibling repositories as source context; do not modify their skills, agent
files, source, or automation as part of a port. Audit imported commands for
working directories, cleanup, generated outputs, network use, and publication.
Do not retain another repository's names, topology, or tooling assumptions.

Keep `AGENTS.md` focused on orientation, shared invariants, and broad routing.
Keep human onboarding and user instructions in the existing `README.md`,
`USER_GUIDE.md`, and `pkg/README.md`. Put contributor engineering contracts in
their owning skills or linked references. Do not move unrelated human
documentation merely to reorganize the handbook.

Use this ownership map when choosing where a durable rule belongs:

| Concern | Owner |
| --- | --- |
| CLI, app composition, shared lifecycle and cross-package invariants | `implementation-architecture` |
| Go design, errors, concurrency discipline and serialization classification | `coding-directives` |
| Runner, protocol, occurrence state, bound status and exit precedence | `implementation-test-pipeline` |
| Profiles, exact thresholds, selected-toolchain cover and local Git baseline | `implementation-coverage` |
| Presentation, assets, redaction, secure artifact publication and manifests | `implementation-reporting` |
| Commands, fixtures, native CI, packaging and release procedures | `scripts-and-automation` |
| Handbook ownership, routing, metadata and validation | `skill-authoring` |
| Commit-message conventions and message files | `source-code-management` |

## Ground guidance in this checkout

Trace a runtime change from `pkg/cli` through `pkg/app` to its focused owner
and the relevant result, report, or artifact boundary. Treat the selected child
Go executable and its raw bytes as primary evidence. Do not infer implemented
behavior from an option field, a renderer template, or a test name alone.

Inspect representative source and test assertions. Preserve the contracts for
raw capture, authoritative status, explicit incompleteness, bounded retention,
statement-weighted coverage, deterministic output, and secure publication in
the owners that enforce them. Describe normal behavior and material failure
paths with observable outcomes.

Distinguish parser/unit evidence, real subprocess integration, CLI fixture
runs, selected-toolchain coverage generation, browser inspection, and native
process-tree tests. Cross-compilation does not prove cancellation on another
OS; HTML string checks do not prove browser interaction or accessibility.
Identify missing evidence without claiming the feature is absent merely
because a suite was not run during a documentation change.

Check command names, defaults, pins, outputs, and side effects against
`Makefile`, `go.mod`, `.github/workflows`, and the selected source. Document
unimplemented automation as unavailable. Keep full test logs and one-off
results outside the durable handbook.

## Metadata and resources

Keep frontmatter to `name` and `description`; include concrete `Use when`
triggers. Match the directory name exactly. Keep UI strings quoted, display
names meaningful, short descriptions 25–64 characters, and default prompts
explicitly invoking the exact `$skill-name` for a representative task.

Read the installed skill-creator metadata reference before changing an
interface. Its generator replaces the whole file: preserve existing policy
and dependencies if present. Do not regenerate unchanged interfaces. The local
validator currently accepts only the three standard interface fields; if a
requested metadata extension exceeds that subset, update the validator
deliberately instead of silently deleting valid configuration.

Keep references directly discoverable with a task trigger. Add neither unused
directories nor creation logs, extra README files, or unfinished placeholders.
Keep source-specific detail out of generic skill discovery descriptions.

## Validation scope

Run `make skills-check` as the dependency-free CI authority for frontmatter,
the current UI subset, placeholders, exact skill targets, canonical topology,
reachability, and cycles. Its implementation lives in
[scripts/skillcheck](../../../../scripts/skillcheck/). It checks entrypoints and
metadata, not all supporting reference links, source claims, or behavior.

Run the installed `quick_validate.py` for every changed skill when its Python
YAML dependency is available. Report an unavailable supplementary validator
explicitly; do not weaken repository validation or install unrelated tools to
hide the limitation. Check quoted UI values, exact invocation, all changed
relative links, reference callers, and source paths separately.

For skill-only changes, those checks, source comparison, realistic routing
walkthroughs, and `git diff --check` are sufficient. Run runtime or automation
suites only when their implementation changes or qualification is part of the
task. Validate release mechanics in disposable repositories with local bare
remotes; never publish as a validation step. If an intentional topology change
alters `scripts/skillcheck`, update its expectations and meaningful malformed
handbook tests together.
