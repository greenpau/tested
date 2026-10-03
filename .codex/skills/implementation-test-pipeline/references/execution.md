# Execution and Cancellation

## Execute and capture

1. Accept the Go executable, working directory, environment policy, and argv as
   separate values. Preserve every Go argument after `--` exactly and avoid a
   shell.
2. Prepare secure evidence files before starting the child. Create rather than
   follow evidence destinations and refuse unsafe managed paths.
3. Tee stdout bytes to `test_output.jsonl` before protocol framing. Tee stderr
   bytes to `stderr.log` before diagnostic or console processing. Propagate
   write failures to orchestration without discarding child status.
4. Stream incrementally with bounded buffers and backpressure. Do not hold the
   complete run in memory merely to decode or display it.
5. Wait for the child and drain owned streams. Preserve numeric exit status,
   signal termination, start failure, wait failure, and cancellation as
   distinct evidence.

Keep live presentation errors separate from raw-write errors. Write stderr
bytes first, then display only captured bytes; propagate raw short writes,
but latch one console error and continue draining both streams after display
failure. Own and join the app's status worker on every return path, including
early setup failures. Retain bounded previews without truncating raw evidence.

Use a time-bounded subprocess handshake to prove progress arrives before child
completion and after raw capture. Check a real quiet-period heartbeat, safe
concurrent stdout/stderr display, cancellation, and the worker's completion.
See [live orchestration tests](../../../../pkg/app/progress_test.go).

## Cancel the complete process tree

Give the child a process-group or platform-equivalent ownership boundary. On
context cancellation:

1. mark the run cancellation cause;
2. request graceful termination for the owned process tree;
3. continue draining stdout and stderr;
4. after a bounded grace interval, force termination of remaining descendants;
5. wait, reap, close streams, and finalize active results as incomplete.

Use a two-second graceful interval when no internal override is configured.
Bound post-exit pipe draining and forced-termination waits as well as the first
signal interval.

Handle races between natural exit and cancellation idempotently. Never signal a
reused process identifier after ownership has ended, and never return while an
owned child or pipe goroutine can still mutate artifacts.

Use a non-reaping exit observer where the platform provides one. Commit the
wait-state transition that disables process-tree signaling before `Cmd.Wait`
releases the stable child identity. On Windows, observe the process handle
becoming signaled before reaping and retain an assigned Job Object through
post-wait cleanup. If the observer fails, fail closed by terminating the owned
tree before disabling signaling and reaping, and bound that fallback reap in
case termination also fails. Never pass a reaped numeric PID to `taskkill`. If
Job Object assignment fails, preserve the ownership error and make only a
bounded pre-reap `taskkill` attempt while the original process handle still
reserves the leader PID. After reaping, use only the stable Job Object and
preserve any ownership or cleanup error.

Treat test-helper control files as bounded protocols. Publish content-bearing
records completely to a same-directory temporary file and atomically rename
them into view; require an explicit completion delimiter and retry absent,
empty, or unterminated observations until a deadline. Use existence-only
markers only when their contents are irrelevant and install the guarded state
before creating them. On setup failure, cancel and join the helper, and disable
numeric-PID cleanup immediately after observing that the process is gone. Have
the parent signal a blocking helper and bound its wait; do not let a helper race
asynchronous self-signal delivery against a fallback exit.

Classify programmatic cancellation, deadline expiry, SIGINT, and SIGTERM
separately. Preserve the observed child status, but project cancellation to the
documented shell result (`130` for programmatic/deadline/SIGINT and `143` for
SIGTERM). Apply cancellation precedence through coverage and report generation,
not only while the Go child is running, and remove or withhold the manifest
when cancellation prevents a coherent publication.

## Verification entrypoints

Inspect [runner implementation](../../../../pkg/runner/) and its controlled
subprocess tests. The [wait-state tests](../../../../pkg/runner/wait_test.go)
exercise signaling/reap ownership; native Unix and Windows tests qualify the
platform mechanisms. Cross-compilation alone is not process-tree evidence.
