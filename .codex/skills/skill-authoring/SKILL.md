---
name: skill-authoring
description: Create, port, revise, or audit tested repo-local skills and their engineering contracts. Use when changing skill ownership, task routes, metadata, source-grounded guidance, or durable lessons from a working session.
---

# Skill Authoring

## Inherit the default authoring workflow

Read the installed `$skill-creator` completely before applying this skill.
Locate it through the active skill catalog. Retain its naming, initialization,
frontmatter, resource, metadata, validation, and behavioral testing guidance;
add the repository requirements below without weakening those defaults.

Edit existing skills in place. Initialize a new skill only when an independent
concern has no suitable owner. Keep changes within the user's requested scope
and preserve existing authorization and invocation policy.

## Apply repository guidance

Read [the tested supplement](references/tested.md) for documentation ownership,
source grounding, metadata conventions, and local validation. Read
[the routing contract](references/hierarchy-contract.md) when creating,
moving, routing, or auditing skills. This entrypoint and the routing contract
govern where repository-specific guidance differs.

Store discoverable skills as siblings at `.codex/skills/<skill-name>/`.
Delegate through `Use [skill-name](relative/path/to/SKILL.md) to <task>.`
statements at the narrowest owner that can select the task. Keep broad routes
in `AGENTS.md`; let broader skills route narrower work. Supporting references
identify related material without creating hierarchy or requiring a reload of
an already applied parent.

## Author engineering contracts

Write the durable knowledge needed to implement, review, operate, and verify
the project. Make responsibility and exclusions clear, then describe relevant
inputs, outputs, lifecycle, ordering, bounds, invariants, failure and recovery,
integration boundaries, and observable acceptance scenarios.

Ground behavior in the selected source and tests. Link useful implementation
and verification entrypoints without copying code or cataloging private
symbols. Name algorithms only where correctness or compatibility depends on
them. Distinguish the required contract from implemented, partial, unavailable,
or unverified behavior.

Keep shared rules in the broadest relevant owner and specialized rules in the
narrowest one. Use imperative language. Keep substantial conditional detail in
references linked beside their task triggers; do not make every task load
every reference. Add scripts for repeated deterministic work and assets only
when a workflow consumes them. Remove unused scaffolding and duplicate prose.

## Update guidance at the end of each code-changing turn

Before sending the final response for any turn that changes code, compare the
final implementation diff with the owning repo-local skills and linked
references. Include tests, embedded assets, dependencies, scripts, and CI.
Perform this review after the last implementation change, even when the task
remains partial or verification fails. Do not defer maintenance to task
completion, a later turn, or a separate user request.

Update affected guidance in that turn: inputs, defaults, lifecycle, failures,
compatibility, operations, examples, and verification. Repair obsolete links,
remove superseded instructions, and validate the changed skills and affected
links before the final response. Briefly report which owners were updated.

Use actual implementation and observed test evidence. Preserve explicit limits
when conformance is partial. If the existing guidance remains accurate and
complete, leave it unchanged and briefly explain why no update was needed in
the final response. Do not create a separate skill or expand a narrow edit
merely to record that work occurred.

## Derive durable guidance from a session

Inventory explicit requests, failures and reproduction evidence, user
corrections, operating preferences, and implementation changes. Classify each
as a durable contract, reusable troubleshooting procedure, implementation
evidence, conformance gap, or transient artifact.

Map durable items to existing owners and compare their source, tests, and
current guidance before revising them. Preserve user intent as observable
behavior and acceptance evidence. Keep session identifiers, credentials,
temporary paths, timestamps, and one-off outputs in working artifacts. Do not
describe a desired but unimplemented capability as available.

## Keep diagrams optional

Make prose, examples, and acceptance scenarios sufficient to understand and
validate a skill. Do not install diagram tooling or add diagram assets as part
of ordinary authoring. If the user requests a diagram, keep it consistent with
the owning contract, render and inspect it with available tools, and keep
disposable previews in `tmp/`. That request does not expand source or tooling
scope.

## Revision workflow

1. Read `AGENTS.md` and follow routes relevant to the task. Inventory all skills
   and metadata for a catalog-wide rewrite or an ownership/routing change.
2. Inspect the source, tests, commands, or user correction establishing the
   behavior. Select the narrowest existing owner and any relevant references.
3. Edit the contract and acceptance scenarios. For a port, adapt terminology,
   paths, examples, tools, and side effects to this checkout.
4. Add or update precise forward routes only when delegation changes. Avoid
   ancestry sections, parent reloads, and empty routing sections in leaves.
5. Update `agents/openai.yaml` only when its interface changes. Preserve
   existing policy and dependency fields; use the default metadata workflow.
6. Run the default skill validator for changed skills when available and
   `make skills-check` for the repository contract. Check affected reference
   and source links separately; neither validator establishes source truth.
7. For ownership changes or a full audit, traverse every root-to-leaf route and
   try representative requests. For an isolated prose fix, review affected
   links and behavior without unrelated catalog or runtime work.
8. Report the changed repository-relative routing chains, validation results,
   and material evidence limits.

For a substantial rewrite, forward-test representative tasks with a fresh
agent when available and safe. Supply the skill path, realistic request, and
minimum raw artifacts, without the intended answer or prior diagnosis. Keep
evaluation read-only or in a disposable workspace and revise only for observed
problems.

## Completion criteria and acceptance scenarios

Finish with valid metadata, concrete discovery triggers, existing link targets,
acyclic actionable routes, and ownership, behavior, and verification guidance
that agrees with source. Catalog audits must establish reachability for every
skill, including informal instructions that a graph validator may miss.

- A narrow correction stays with its owner, preserves invocation policy, and
  needs neither a new skill nor a diagram.
- A new concern is discoverable from a representative request through the
  narrowest appropriate router without loading unrelated leaves.
- Moving guidance preserves each durable rule, repairs inbound links, and
  removes obsolete copies. Unresolved limits remain explicit.
- A turn changes code and ends with partial work or failing checks: the owning
  guidance still reflects the resulting behavior and verification limits
  before the final response. A later code edit triggers another review.
- Passing metadata checks does not establish runtime conformance. Compare
  representative outputs with source/tests and identify missing end-to-end or
  native-platform evidence honestly.
