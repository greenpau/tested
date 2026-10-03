# Canonical Coverage HTML

## Preserve compatibility artifacts

Keep `.coverage/coverage.out` as the raw Go-produced compatibility profile.
Do not reorder, normalize, redact, or replace it merely to simplify parsing.
Create it with managed-file protections and expose its digest and size through
the manifest.

Generate `.coverage/coverage.html` with the selected Go executable:

```text
go tool cover -html=<absolute-profile-path> -o=<staged-output-path>
```

Set the command working directory to the tested project directory. Relative
source paths in a profile belong to that directory, not `.coverage` and not the
caller's unrelated current directory. Create the staged HTML on the
destination filesystem and require a successful command.

Do not substitute a custom coverage renderer. Stream the Go-authored document
through the fixed `pkg/report` presentation decorator into a second
destination-filesystem temporary file. Bound the search for the unique closing
`head`, reject a pre-existing tested theme marker within that head or multiple
closing heads, and inject only the tested-owned viewport declaration,
content-security policy, embedded theme CSS, progressive explorer JavaScript,
and optional safely encoded bounded comparison model. Do not parse, redact,
reorder, or regenerate the source, coverage spans, file selector, or
Go-authored script; the annotated source body may itself contain the marker
text. Revalidate both temporary files against replacement before publishing
the decorated file atomically with mode `0600`.

The supported Go templates use the case-sensitive `</head>` byte anchor. Limit
the buffered prefix through that anchor to 1 MiB, then stream the remaining
body with only bounded overlap needed to reject a second closing head.

The fixed assets must contain no external resource reference. Treat an optional
comparison payload as untrusted data, encode it contextually, and never admit
it through a trusted-content cast. Removing the exact rendered injection from
a completed artifact must restore the selected Go toolchain's bytes exactly.
Source the decorator's HTML fragment, shared CSS, and explorer JavaScript from
the report package's `embed.FS`; renderer construction must return an
asset-loading error rather than reading runtime files or panicking.

## Failure and recovery

- Preserve the profile and child status when parsing fails.
- Bound total profile bytes, source-file cardinality, block cardinality,
  individual lines, and diagnostic text for hostile profiles.
- Remove or leave unadvertised both canonical and decorated temporary outputs
  after a failed cover command or decorator; never publish a truncated file as
  complete and never replace an existing report on failure.
- Propagate context cancellation to `go tool cover` and reap it using the same
  process-lifecycle standards as other child work.
- Wrap tool stderr and exit information without leaking redacted secrets or
  discarding the underlying cause.
- Advertise coverage HTML in the manifest only after its atomic publication and
  digest calculation succeed.

## Verification entrypoints

Inspect [cover invocation](../../../../pkg/coverage/html.go),
[cover integration tests](../../../../pkg/coverage/html_test.go), and
[decorator tests](../../../../pkg/report/coverage_html_test.go). Validate exact
injection removal against real selected-toolchain bytes, not only a synthetic
head. Presentation design belongs to the report-owned HTML contract.
