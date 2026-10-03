# Bound Status, Offline Evidence, and Exit Policy

## Persist bound status

After raw files close, persist a bounded `run.json` that binds the status to
the exact event, stderr, and optional coverage bytes by size and SHA-256.
Record whether the child started, whether exit status is known, whether
capture completed, the monotonic child span as `duration_ns` when available,
and every fatal run-integrity issue. Encode one strict `tested/run/v1`
document no larger than 1 MiB; reject duplicate JSON members at every depth,
unknown fields, trailing JSON values, inconsistent cancellation/timing
state, duplicate or unmanaged bindings, and a missing `test_output.jsonl`
binding. Accept only interrupt→130 and terminated→143 for signal
cancellation. Permit at most the three canonical evidence names and sort
bindings lexically when encoding.

Require a `coverage.out` binding whenever recorded coverage policy is
available. Unavailable coverage cannot satisfy a policy; an available policy
must retain a nonempty actual percentage and nonzero statement count. Removing
only the profile binding would invalidate that status, even if the recorded
minimum was satisfied.

## Apply failure precedence

Preserve all causes, then select command status in this order:

1. cancellation (`130` for programmatic, deadline, or SIGINT; `143` for
   SIGTERM);
2. authoritative nonzero child exit or signal projection, preserved exactly
   unless coherent offline `--allow-failures` permits exit 1;
3. start, wait, raw capture, protocol, result-integrity, or incomplete-evidence
   failure (`2`);
4. unallowed failing event evidence (`1`), except that failure evidence
   conflicting with a zero child exit is an integrity failure (`2`);
5. an exact unmet minimum-coverage policy after a coherent successful child
   (`3`);
6. report rendering or artifact-publication failure (`2`);
7. success (`0`).

Use platform-appropriate signal or exit projection when possible. If several
failures share a run, keep the authoritative child status and attach later
failures to diagnostics and summary metadata. A parsed `fail` with a zero child
exit is an integrity conflict, not permission to report success.

Treat live-console failures as presentation failures, including errors during
coverage, derivative publication, manifest hashing, or the final stage. Never
persist those errors as fatal child/run evidence in `run.json`. Collect the
first error, stop and join the status worker before returning, refresh report
assessments when necessary, and remove the current generation's manifest on a
late failure. Do not delete an existing bundle when an early input/setup
failure has not prepared a new generation. Verify raw bytes and child exit
precedence survive display failure and that a later offline rerender recovers.

The `report` command has no new child status. Copy explicitly selected external
event, stderr, and coverage evidence into their canonical managed names before
analysis. Import bound same-named stderr/profile siblings from an explicitly
selected external status bundle when no explicit companion overrides them.
Remove stale managed companions that are not selected or bound, while leaving
unknown files untouched.

Trust imported status only when the selected strict `run.json`
cryptographically binds the selected evidence. Re-encode an external valid
status document canonically at the managed `run.json` path, but do not rewrite
a status file already at that exact managed path during an ordinary rerender.
For a policy-free `report --no-coverage`, remove the profile and its binding,
canonically rewrite managed status only when that projection changes it, and
preserve all child/cancellation/issue fields and retained evidence bindings.
If status records a coverage policy, reject `report --no-coverage` before
artifact preparation so the command cannot erase an authoritative decision or
mutate the bundle. Render a bare log as incomplete for inspection, return
infrastructure failure, and withhold the manifest. Never consume an unrelated
default `run.json` for an explicit custom event stream.

## Verification entrypoints

Inspect [runstatus](../../../../pkg/runstatus/evidence.go) and
[its schema tests](../../../../pkg/runstatus/evidence_test.go) for strict
encoding/decoding. Compare [app status handling](../../../../pkg/app/run_status.go),
[offline reporting](../../../../pkg/app/report.go),
[status tests](../../../../pkg/app/run_status_test.go), and
[exit tests](../../../../pkg/app/exit_test.go) for evidence selection and policy.
