# Security Policy

## Supported versions

Security fixes target the latest tagged release and the current `main` branch.
Older releases may receive fixes when the same change can be backported safely;
operators should otherwise upgrade to the newest release.

## Reporting vulnerabilities

Report suspected vulnerabilities privately through GitHub's
**Security → Report a vulnerability** form when it is available, or email
`greenpau@outlook.com` with a subject beginning `[tested security]`. Do not open
a public issue containing exploit details, test credentials, or report
artifacts.

Include the tested version or commit, operating system and architecture, Go
version, minimal reproduction, impact, and the affected artifact or command
boundary. Share sensitive evidence through a mutually agreed private channel;
do not attach a complete `.coverage` directory until its contents have been
reviewed.

## Report threat model

`tested` processes content controlled by source repositories and their tests:

- package and test names;
- test standard output and standard error;
- compiler, vet, panic, race, and timeout diagnostics;
- file and module paths;
- coverage profiles and complete covered source files.

That content is untrusted. Generated HTML uses contextual escaping and does not
load network assets. Report targets and the artifact root may not be symbolic
links. Derived files are published through same-directory temporary files.
Before a manifest accepts an artifact, tested hashes an opened regular file and
rechecks that the managed pathname still identifies the same stable object.
On POSIX systems it rejects a managed regular file with multiple filesystem
links before changing its mode or hashing it, so an external hard-link alias is
not silently chmodded or accepted into the manifest. External import sources
are copied through an independent same-directory temporary file and are not
chmodded.
`run.json` separately binds process status to the sizes and SHA-256 digests of
the exact event, stderr, and coverage evidence retained for that run. Its
`tested/run/v1` decoder is limited to 1 MiB, rejects duplicate members at every
depth, unknown fields, and trailing JSON values, requires the event binding,
and validates canonical cancellation/exit mappings, duration, file-name, size,
digest, and coverage-policy relationships before offline status is trusted.

Decoded Go events have an independent forward-extension budget: at most 64
schema-unknown fields and 64 KiB of decoded unknown-field names plus exact
retained values per record. Duplicate decoded member names and extension
budget exhaustion are malformed semantic evidence, while the raw event stream
remains unchanged.

The default artifact root uses mode `0700`; managed files use mode `0600`.
Existing access-control policy outside that root, CI artifact permissions,
backups, and uploaded copies remain the operator's responsibility.
Windows does not interpret POSIX mode bits as Unix discretionary access
control; use an appropriate Windows ACL and isolated CI workspace there.

## Sensitive evidence

Every file in `.coverage` can reveal project identity or sensitive data. In
particular, `test_output.jsonl`, `stderr.log`, `run.json`, derived test reports,
and `coverage.html` can contain credentials, tokens, cookies, command
arguments, private paths, customer data, or proprietary source. `--redact`
affects plain/Markdown/JSON console output, `test_output.html`, `summary.json`,
`junit.xml`, and `index.html`. It does not silently modify live-captured or
imported event/stderr/profile bytes, annotated coverage source, `run.json`, or
manifest-integrity fields.

Before sharing `.coverage`:

1. Inspect raw test and stderr output.
2. Decide whether annotated source may leave the development environment.
3. Apply explicit redaction expressions and review their result.
4. Upload only the necessary artifacts.
5. Restrict retention and download permissions in the CI system.

Do not treat filesystem mode bits as encryption.

## Execution boundary

`tested` invokes the selected Go command with an argument vector and never
constructs a shell command from repository-controlled input. Go tests are
arbitrary native code and execute with the invoking user's permissions.
`tested` is a reporter, not a sandbox. Run untrusted repositories inside an
appropriate container, VM, or managed CI isolation boundary.

On Unix, cancellation targets the owned process group, permits the default
two-second graceful interval, and then applies bounded forced termination,
pipe draining, and reaping. On Windows, tested assigns the child tree to a
kill-on-close Job Object when host policy permits it and uses bounded
`taskkill`/process termination as a compatibility fallback. Cancellation also
applies while coverage and reports are generated; a cancelled command does not
publish a coherence manifest. CI and other hosts may impose a pre-existing
restrictive Job Object. Operators should still verify that hostile test
subprocesses cannot outlive the surrounding container, VM, or job isolation
boundary.

## Evidence coherence

Treat `manifest.json` as the final report-set coherence marker. A missing
manifest, a digest mismatch, an unbound live-captured or imported evidence
file, or a `run.json` issue marked fatal means the generation is incomplete,
even when individual HTML or XML files open successfully. A bare archived
event stream has no authoritative child status; tested can render it for
inspection but reports an incomplete outcome and does not publish a success
manifest.

Artifact presence is conditional. Both coverage files are absent when coverage
is disabled. An unavailable profile remains absent; a nonempty invalid profile
may remain as evidence without `coverage.html`. An imported bundle may omit
unbound stderr or coverage companions. Offline report mode copies selected
external evidence into canonical managed names, verifies the selected bundle,
and removes stale managed companions that are not selected or bound. It does
not recursively clean the output directory or remove unknown files.

For policy-free status, a successful offline `--no-coverage` projection removes
the profile binding from the managed canonical `run.json` while preserving
authoritative child status and all retained bindings. Status that records a
coverage policy cannot be combined with `report --no-coverage`: tested rejects
the command before artifact preparation and leaves the managed bundle
unchanged, rather than discarding the evidence for a prior coverage decision.

`run.json` and `manifest.json` provide integrity checks, not authenticity.
Another process with the invoking user's filesystem permissions can replace
both evidence and metadata. The single-link checks reject preexisting aliases
and recheck after sensitive operations, but POSIX does not provide an atomic
"verify link count and chmod" operation; a concurrent same-user process can
create a hard link in that interval and observe the mode change before tested
detects the race. These controls protect routine publication coherence, not a
workspace already controlled by an active peer process. Use signed CI
provenance or an external content-addressed store when protection from a
compromised workspace is required.
