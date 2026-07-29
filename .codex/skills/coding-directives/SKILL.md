---
name: coding-directives
description: Apply tested repository Go coding standards to implementation and review work. Use when creating or modifying Go packages, APIs, process execution, protocol parsing, result models, coverage logic, renderers, artifact storage, errors, security-sensitive paths, or tests in tested.
---

# Coding Directives

Adapt the established `go-authcrunch` Go style to a standalone test tool:
prefer cohesive types with methods, small consumer-owned interfaces, explicit
package ownership, standard-library building blocks, wrapped errors, and
focused table-driven tests.

## File and package discipline

- Add the repository Apache License 2.0 copyright header to every new Go source
  and test file. Match the wording and year convention already present in the
  repository.
- Put orchestration and exit policy in `pkg/app`, command grammar in `pkg/cli`,
  child lifecycle in `pkg/runner`, wire decoding in `pkg/protocol`, normalized
  state in `pkg/result`, profile semantics in `pkg/coverage`, projections in
  `pkg/report`, durable process outcome in `pkg/runstatus`, and managed
  publication in `pkg/artifact`.
- Keep `main.go` limited to process setup, app invocation, and final exit.
- Keep exported surfaces minimal. Export a symbol only when another package
  needs the contract; keep helpers and concrete adapters unexported.
- Store report-owned HTML, CSS, and JavaScript sources under
  `pkg/report/assets/`, compile them with the standard `embed` package, and
  parse them from the embedded filesystem. Return asset parse/assembly errors
  from renderer construction; never depend on runtime asset paths or hide
  whole pages in Go raw-string literals.
- Avoid import cycles and convenience packages that mix unrelated ownership.
  Move a shared primitive only when its ownership is genuinely cross-domain.

## Design types around behavior

- Use cohesive structs with methods for runners, framers, aggregators, profile
  sets, renderers, publishers, and application services.
- Keep each struct responsible for one lifecycle or policy. Split types that
  simultaneously execute, parse, aggregate, render, and publish.
- Define small interfaces in the consuming package. Prefer the concrete type
  when substitution is unnecessary, and compose behavior instead of creating a
  broad framework interface.
- Use utility functions only for stateless, narrowly scoped transformations
  that do not naturally belong to a type.
- Pass configuration through constructors or explicit option structs. Validate
  required dependencies and bounds before starting external work.
- Avoid package-global mutable state. Treat reusable renderers and parsers as
  immutable or create them per run.

## Context, processes, and concurrency

- Place `context.Context` first on operations that execute, wait, stream,
  publish, or may block. Propagate it unchanged unless a documented child
  lifetime requires a derived context.
- Start commands from an argv slice with `exec.CommandContext` or a package
  abstraction that preserves argv boundaries. Never build a shell command from
  user-controlled arguments.
- Own every goroutine, pipe, timer, and process. Provide a finite completion
  path, propagate cancellation, drain streams, stop timers, close owned files,
  and wait for goroutines before returning.
- Isolate platform-specific process-tree behavior behind small files or types
  with build constraints. Preserve the same cancellation contract on every
  supported platform.
- Avoid channels without a documented producer, consumer, close owner, and
  bounded buffering policy.

## Errors and status

- Return errors instead of panicking. Permit panic only in a test helper whose
  sole purpose is to fail test setup immediately.
- Wrap errors at ownership boundaries with `%w` and useful operation context:
  for example, start Go tests, decode an event, parse a profile, render JUnit,
  or publish an artifact.
- Preserve typed or inspectable causes for child exit, signal, cancellation,
  framing, coverage, threshold, rendering, and publication failures. Do not
  reduce all failures to strings.
- Keep cleanup errors when they affect evidence integrity. When several causes
  occur, retain them with `errors.Join` or an explicit result while applying
  the app's status precedence separately.
- Do not use a later report error to overwrite a child failure. Do not return
  success merely because a partial report was written.

## Evidence and security

- Write captured child bytes before parsing them. Do not trim, normalize,
  recolor, redact, or re-encode the compatibility JSONL evidence.
- Create managed directories with `0700` and files with `0600`; apply explicit
  modes rather than relying on a permissive umask.
- Resolve and validate managed paths beneath the output root. Reject traversal,
  symlink escapes, special-file targets, and unsafe replacement boundaries.
- Escape text for its actual destination: terminal controls, HTML, JSON, XML,
  Markdown, and filesystem metadata have different rules.
- Apply configured redaction to live and derived presentations. Keep exact raw
  evidence private and make its sensitivity explicit in the manifest rather
  than silently altering it.
- Avoid logging environment values, command secrets, raw test payloads, or
  unredacted diagnostics outside the secured evidence boundary.

## Determinism and data modeling

- Sort map-derived output with explicit stable keys. Preserve source order only
  when source order is part of the contract.
- Represent package, test, action, occurrence ordinal, terminal state,
  diagnostic provenance, and duration quality explicitly. Avoid sentinel text
  for semantic states.
- Distinguish absent, zero, incomplete, and malformed values. Do not convert a
  missing duration to a measured zero or missing coverage to zero percent.
- Use integer statement weights and counts for coverage calculations; convert
  to a percentage only at the presentation boundary with documented rounding.
- Make serialization schemas versionable and keep optional fields meaningful.
  Avoid timestamps, random IDs, host paths, or map order that make equivalent
  result fixtures differ unnecessarily.

## Standard library first

Prefer `os/exec`, `context`, `encoding/json`, `encoding/xml`, `html/template`,
`io`, `bufio`, `path/filepath`, `errors`, `time`, and `crypto/sha256` before
adding dependencies. Add an external module only when it materially improves a
required contract, review its license and maintenance posture, and keep it
behind a package-owned boundary.

## Tests and verification

- Add table-driven tests beside the owning package. Name cases by behavior and
  cover success, boundary values, malformed input, cancellation, and cleanup.
- Use `t.TempDir`, `t.Setenv`, deterministic clocks/readers, and fake
  consumer-facing interfaces. Do not depend on the developer's module cache,
  global environment, terminal, or network.
- Exercise real subprocess helpers only through controlled test binaries and
  time-bounded contexts. Verify process-tree cleanup without leaving children.
- Add golden files only for stable user-visible formats. Normalize permitted
  variability, compare exact bytes, and require an explicit update action.
- Test escaping and redaction with hostile HTML, XML, Markdown, ANSI, path, and
  secret-shaped inputs. Test modes and symlink rejection on supported systems.
- Run `gofmt` on every changed Go file, focused package tests, and `go test
  ./...`. Before integration, run race-enabled tests and cross-build the
  supported operating-system and architecture matrix.
- Treat `go vet`, race, and cross-build failures as implementation defects or
  explicitly documented platform exclusions; do not hide them in automation.
