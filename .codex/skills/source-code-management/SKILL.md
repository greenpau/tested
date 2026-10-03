---
name: source-code-management
description: Draft and review tested commit messages and message files. Use when summarizing an intended commit, choosing an indicator, or checking the required subject and Before/After/Tests/More info structure.
---

# Source Code Management

Own commit-message content and review artifacts. Keep staging, commits, tags,
and pushes separate from message drafting; honor the user's explicitly
requested Git operation when one is part of the task.

## Inspect the intended change

Read `git status --short`, the staged diff, the unstaged diff, and relevant
untracked files before drafting a message. Inspect recent subjects when they
provide useful repository precedent. Base claims on the actual intended commit
scope and observed verification.

Do not stage, unstage, commit, amend, tag, or push unless the user explicitly
requests that separate action. A request for a commit message authorizes only
the message artifact.

## Write the required subject

Use this form:

```text
indicator: concise imperative outcome
```

Apply every subject rule:

- use exactly one lowercase indicator and no parenthesized scope;
- keep the complete line shorter than 87 characters;
- use imperative present tense;
- describe the resulting change rather than the work session;
- omit the trailing period;
- select the narrowest owner of the primary behavior.

## Select a tested indicator

Prefer a product-owner indicator:

- `app`: orchestration, run/report lifecycle, and exit precedence;
- `cli`: command grammar, flags, help, version, and Go-argument separation;
- `runner`: child execution, raw capture, cancellation, and process trees;
- `protocol`: bounded framing and `TestEvent`/`BuildEvent` decoding;
- `result`: occurrence aggregation, state, timing, and summaries;
- `coverage`: profile parsing, weighted totals, thresholds, and cover HTML;
- `report`: console, HTML, JSON, JUnit, index, escaping, and redaction;
- `artifact`: managed paths, permissions, atomic publication, and manifests.

Use a maintenance indicator when it is the honest center:

- `fix`: correct tested behavior without a known shipped regression;
- `breakfix`: repair a reported shipped regression, panic, or data-loss issue;
- `feat`: add user-visible capability with no narrower product owner;
- `tests`: primarily add or revise tests, fixtures, or golden evidence;
- `docs`: change user-facing documentation only;
- `skills`: change `AGENTS.md`, repo-local skills, or agent metadata;
- `refactor`: restructure implementation without intentional behavior change;
- `security`: harden execution, paths, permissions, redaction, or disclosure;
- `build`: change Make, compilation, packaging, or local build behavior;
- `github`: change GitHub Actions, templates, or repository GitHub metadata;
- `ops`: change dependencies, toolchain, versioning, release, or maintenance
  configuration;
- `various`: intentionally combine unrelated work that cannot be split.

If several indicators fit, choose the user-visible or architectural center.
Prefer splitting unrelated changes over `various` when the user controls scope.

## Write the required body

Include these sections in this exact order:

1. `Before this commit:`
2. `After this commit:`
3. `Tests:`
4. `More info:`

Separate sections with one blank line. End each label with a colon. Keep lines
at or below 87 characters except unavoidable links and detailed `More info`
content.

- Describe the previous observable behavior or repository state under
  `Before this commit`.
- Describe the resulting behavior, contract, or state under
  `After this commit`.
- Name exact commands and results under `Tests`.
- Write `Tests: not run — <reason>` when verification did not run.
- Summarize important design decisions, compatibility, security, generated
  outputs, and caveats under `More info`.
- Never claim a command passed unless its result was observed or supplied by
  the user.

After the required sections, add only applicable optional sections in this
order:

1. `Resolves:` for issues completely resolved;
2. `Partial Resolution:` for issues partly addressed;
3. `See also:` for related references;
4. `Links:` for a bulleted reference list.

Use valid links in the first three optional sections and separate multiple
links with comma and space.

## Template

```text
indicator: concise imperative outcome

Before this commit: describe the previous behavior, limitation, or state.

After this commit: describe the resulting behavior, implementation, or state.

Tests: name each command and result, or say not run and why.

More info: summarize implementation decisions and material caveats.
```

## Apply tested-specific evidence

- Mention authoritative child-status preservation when execution or exit policy
  changes.
- Mention raw-before-parse evidence and secure modes when runner or artifact
  behavior changes.
- Mention weighted statements rather than a rounded percentage when coverage
  calculation changes.
- Mention exact compatibility artifacts when report or publication behavior
  changes.
- Mention bounded framing, explicit incomplete results, redaction, atomic
  publication, or determinism only when the diff actually implements or tests
  those claims.
- Describe a proposed Make, CI, or release workflow as intended rather than
  present when its files are not in the intended commit.

## Create a commit-message file

When asked to create a commit message for the change, write it below
`tmp/commits` with a `YYYYMMDD_HHMM_` prefix and a concise slug. Treat that file
as an uncommitted working artifact unless the user says otherwise.

After writing it, provide the copy-paste command:

```text
git commit -F tmp/commits/YYYYMMDD_HHMM_<short-slug>.txt
```

Re-read the file, check line lengths and required section order, and tell the
user which staged or unstaged scope it describes. Do not modify the index.

## Acceptance scenarios

- A mixed worktree yields a message describing the selected commit scope,
  without staging unrelated files or claiming all visible changes belong to it.
- Unrun checks appear as unrun with a reason; a proposed workflow is not
  described as implemented or passing.
- A skill-only change uses the `skills` indicator, keeps the imperative subject
  below 87 characters, and includes the four required body sections in order.
- A message-file request creates a reviewable `tmp/commits` artifact and a
  usable `git commit -F` command while leaving the index unchanged.
