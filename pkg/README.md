# Package ownership

`tested` keeps one normalized test result between execution and every report
adapter. Packages are organized by behavioral owner:

| Package | Responsibility |
| --- | --- |
| `app` | Compose commands, lifecycle, artifact publication, and exit policy |
| `cli` | Parse tested options and preserve the Go argument boundary |
| `runner` | Execute and cancel the owned `go test` process without a shell |
| `protocol` | Frame and decode Go test/build JSON while retaining raw evidence |
| `result` | Correlate packages and occurrence-aware test outcomes |
| `artifact` | Secure managed paths, atomic writes, hashes, and manifests |
| `coverage` | Parse profiles, calculate weighted totals, and invoke Go cover |
| `report` | Render terminal, HTML, JSON, JUnit, and index projections |
| `runstatus` | Validate durable child status and cryptographic raw-evidence bindings |

Presentation packages must consume normalized results rather than reparsing raw
test output. Primary evidence is never rewritten to satisfy a renderer.
