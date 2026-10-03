# Skill Routing Contract

## Represent task delegation

Keep every discoverable skill in a sibling `.codex/skills/<skill-name>/`
directory. Route broad work from `AGENTS.md` and narrower work from the owner
that can select it. Use exactly this sentence shape, with a concrete action:

`Use [skill-name](relative/path/to/SKILL.md) to perform a specific task.`

Require an imperative `Use`, the exact skill name linked to its `SKILL.md`, and
`to` followed by a selective task or trigger. Keep routes in the router's
entrypoint so they are discoverable before loading conditional references.
Leaves need no hierarchy section or backlink.

Supporting links identify a collaborating contract, reference, or source;
they do not delegate work. Do not disguise delegation as an unlinked skill
name, lowercase instruction, or reference that reloads a parent. Follow
narrower routes only while they apply to the current task. A specialized skill
adds detail without weakening previously applied requirements.

## Preserve the current graph

The current repository topology is:

| Router | Delegates |
| --- | --- |
| `AGENTS.md` | `implementation-architecture`, `skill-authoring`, `source-code-management` |
| `implementation-architecture` | `coding-directives`, `implementation-test-pipeline`, `implementation-coverage`, `implementation-reporting`, `scripts-and-automation` |

Keep specialized skills as leaves until a distinct concern merits independent
loading. Change this topology intentionally with the corresponding
`scripts/skillcheck` expectation. Supporting references are not new skills or
extra graph nodes.

## Review contract content

State responsibility and exclusions, relevant inputs and outputs, lifecycle,
invariants, failure and recovery, collaborating owners, and observable
acceptance scenarios. Adapt the organization to the concern; do not add empty
sections or require identical headings in every skill.

Keep shared vocabulary and dispatch decisions in routers. Put conditional
detail in linked references with explicit read triggers and no duplicated
authoritative copy. Keep required behavior distinct from current conformance;
a test filename is navigation, not proof of acceptance.

## Audit reachability and usefulness

For each discovered skill:

1. Match its frontmatter name to its directory; allow only `name` and
   `description` and require concrete discovery triggers.
2. Follow every actionable route from `AGENTS.md`; require exact existing
   targets, selective actions, full reachability, and no cycles.
3. Inspect informal `Use`, `Load`, and `Read` instructions as well as formal
   routes for hidden delegation or parent reloads.
4. Walk representative runtime, automation-only, authoring, and commit-message
   requests from the root. A reachable skill with the wrong incoming trigger
   is still a routing defect.
5. Check relative source/reference links and callers. Preserve durable
   contracts when moving text and repair inbound references.
6. Validate changed skills and the repository graph using the supplement's
   scoped checks. Review source truth and acceptance evidence separately from
   metadata success.

Do not make a narrow prose correction depend on a full catalog audit, runtime
suite, or diagram. An explicit catalog rewrite or ownership change does
require the full route review.
