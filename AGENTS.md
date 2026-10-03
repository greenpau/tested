# Repository Instructions

This repository develops `tested`, a standalone Go test runner and reporting
tool. It replaces the shell pipeline built from `go test -json`, `tee`,
`tparse`, `go-test-report`, and `go tool cover` with one cancellation-aware Go
process. Repo-local skills are the engineering handbook; follow task routes
and read conditional references only when they apply.

## Shared invariants

- Treat the child Go command and captured bytes as primary evidence. Parsing
  or reporting must never turn a failed, signaled, cancelled, malformed, or
  incomplete run into success.
- Capture raw streams before interpretation. Bound retained interpretations
  without truncating evidence; preserve occurrence identity and explicit
  incomplete state.
- Compute coverage from statement weights and compare thresholds exactly.
- Keep the four compatibility artifacts: `test_output.jsonl`, `coverage.out`,
  `test_output.html`, and `coverage.html` under the managed output directory
  (default `.coverage`). Make coverage presence conditional on valid evidence
  and options.
- Keep managed directories private (`0700`) and managed files private (`0600`).
  Bind durable child status to exact evidence; publish derivatives atomically
  and the coherence manifest last.
- Keep presentations deterministic, escaped, and redactable. Preserve raw
  evidence and Go-authored coverage bytes. Embed report-owned HTML, CSS, and
  JavaScript from `pkg/report/assets/`.

## Project Structure

| Area | Responsibility |
| --- | --- |
| `main.go` | Keep the process entrypoint thin and hand control to the app package. |
| `pkg/cli/` | Own command grammar, option validation, Go-argument separation, and help/version presentation. |
| `pkg/app/` | Orchestrate run and report lifecycles and own final exit-policy precedence. |
| `pkg/runner/` | Execute argv-based Go commands, capture raw streams, propagate cancellation, and reap process trees. |
| `pkg/protocol/` | Frame bounded input and decode the `TestEvent`/`BuildEvent` union without losing raw evidence. |
| `pkg/result/` | Aggregate occurrence-aware package, test, diagnostic, duration, and incomplete state. |
| `pkg/coverage/` | Parse and merge Go coverage profiles, calculate weighted totals, invoke Go cover HTML generation, and stage optional decoration securely. |
| `pkg/report/` | Render live console plus HTML, JSON, JUnit, and index projections; own embedded HTML/CSS/JS assets, the fixed visual layer, coverage decorator, escaping, and redaction. |
| `pkg/artifact/` | Enforce managed paths and permissions, stage atomic publications, hash artifacts, and commit the manifest. |
| `pkg/runstatus/` | Validate durable child outcome metadata and raw-evidence bindings used by offline reporting. |
| `internal/tag/` | Enforce serialization-tag policy and require an explicit classification for every exported production struct. |
| `.codex/skills/` | Store this routed engineering handbook as sibling skills. |

Keep dependencies directed from `pkg/app` into focused owners. Let `pkg/report`
consume normalized results rather than parser internals; let `pkg/protocol`
produce events rather than mutate reports; let `pkg/artifact` own filesystem
publication rather than test semantics. Define narrow interfaces at the
consumer boundary when orchestration needs to substitute an owner in tests.

## Repo-local skills

- Use [implementation-architecture](.codex/skills/implementation-architecture/SKILL.md) to design, implement, review, or audit runtime behavior, package contracts, or repository automation and select the specialized workflow.
- Use [skill-authoring](.codex/skills/skill-authoring/SKILL.md) to create, port, revise, route, validate, or audit repo-local skills and session-derived engineering guidance.
- Use [source-code-management](.codex/skills/source-code-management/SKILL.md) to create or review repository-compliant commit messages and commit-message files.

## Handbook maintenance

Store skills as direct siblings under `.codex/skills/<skill-name>/`. Express
hierarchy through exact, actionable `Use [skill](relative/SKILL.md) to <task>.`
statements. Keep routes forward-only, acyclic, selective, and reachable from
this file; do not add ancestry metadata or routing-only backlinks.

Before the final response of every turn that changes code, review the final
diff against the owning repo-local skills and linked references. Include test,
embedded-asset, dependency, script, and CI changes in this review. Update
affected guidance in the same turn, after the last implementation change; do
not defer it to a later turn or wait for a separate documentation request.
Preserve explicit implementation and verification limits even when work is
partial or checks fail.

Briefly identify the skill updates in the final response. If the existing
guidance remains accurate and complete, leave it unchanged and explain why no
update was needed. Keep human onboarding in the existing user documentation.
Run `make skills-check` for handbook changes; check affected reference/source
links and behavior separately.
