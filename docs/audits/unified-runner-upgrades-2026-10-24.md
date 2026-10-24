# Combined Python runner upgrade proposal

This review branch preserves the component histories from three proposal heads.
It does not merge any application change into main or deploy any workload.

| Component source | Retained scope |
| --- | --- |
| #349, `61d41587db94623b7998aa5819b1789149b385ab` | Evidently/Plotly 7 and Dynaconf ports, Python 3.15.1, telemetry opt-out, existing Pandas adapter, native expansion and strict scans inherited from #343/#348 |
| #346, `9a93c7e4c4eddc5a00577ff7cd67770c49177807` | Real offline Guardrails engine port, native OpenSSL/SQLite clients and strict scan; Agent Framework/Pydantic AI graphs inherited from #339 |
| #342, `81290e02df0e97af4f60e2755ed36d61ad418825` | CrewAI-profile locks and source-built OR-Tools runtime/build locks targeting Python 3.15.1 |

Conflict resolution preserves every byte of the 118 inspected component files
against their respective source heads, including all application, lock, case,
port-builder and native-builder files. The shared native README retains both
component explanations. All 21 existing service test files remain byte-identical;
no application cases are added or weakened.

The shared workflow retains separate dependency installation for the two source
ports and the source-built OR-Tools wheel. Ordinary installation excludes those
three runners, avoiding an earlier incompatible upstream wheel or double install.
The exact-image archive/verifier sources remain unchanged. Identical scan/artifact
step bodies are combined under one eight-runner condition; raw/modified findings
remain blocking without advisory exclusions. Compose changes only the Guardrails
service's named native build context relative to #349.

Metadata resolution retains eight targets: Evidently, Garak, Agent Framework,
Guardrails, Pydantic AI, CrewAI, OR-Tools runtime and OR-Tools build. Each maps to
its actual input and matching interpreter/image profile. Source port wheels are
built and hash-checked before resolution. The disposable resolver still mounts
only public inputs/tools/cache/outputs, uses UID 10001, drops capabilities and
does not receive application source, credentials or a Docker socket.

## Qualification boundaries

Source identity and component CI are evidence for those components. They do not
qualify this combined checkout. Fresh combined metadata/source-tree/lock evidence,
existing SDK/image/health cases and full current-source CI are required.

Remaining known gaps include raw OS/native/Go image findings, SQLitedict's
unpatched advisory, Mini-SWE's constrained Filelock graph and execution boundary,
Protobuf constrained below the current public stable release, older native
clients/compiler modules in other runners, vendor prerelease/compatibility
constraints, and independent privacy/provider/database/server acceptance.
The Plotly port has no complete embedded-frontend visual/typed-array/image-export
qualification. No complete application, portfolio currency or security claim is
made by this combined proposal.

Local verification is limited to source/blob identity, AST/JSON/YAML parsing and
whitespace. No local application/vendor/native/image/scanner suite is run. Keep
the PR draft; main-branch merge approval and deployment remain separate.
