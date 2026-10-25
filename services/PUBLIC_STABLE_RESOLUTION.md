# Current public runner dependency resolution

The public metadata workflow now covers FastMCP, DeepEval and DeepTeam alongside its ten existing profiles. Each new profile uses the same reviewed Python 3.15.1 SDK and normal public dependency resolution. It retains normal Python/version constraints, publisher hashes and failure diagnostics. No current runtime image, installed graph or application case is changed merely to obtain metadata.

The 25 October public release refresh found remaining Starlette/SSE-Starlette and Multidict/wcwidth gaps in these current proposal locks. Generate complete hash locks in the hosted SDK, inspect every selected version and publisher identity, then qualify proposed runtime graphs through the existing cases and exact-image scans before integration. A resolved graph is not evidence of successful imports, full upstream behavior, native compatibility or a clean image.

DeepTeam 1.0.10 still declares Python `<3.14` upstream. Its Python 3.15.1 profile must fail normally if that constraint prevents resolution. Do not suppress the failure, override Requires-Python, downgrade the profile, accept a prerelease as stable or drop the engine. A compatible source port with preserved original upstream cases would require separate preparation and verification. The existing Python 3.13 runtime remains a recorded gap against the current stable interpreter target; it is not relabeled as current.

All thirteen workflow profiles remain required and run with fail-fast disabled. No continue-on-error, original-case filter, assertion edit, source/module scan exclusion or advisory waiver is introduced. Source-port and native/library/image qualification already in progress remains separate. Do not merge this stacked proposal as proof that the portfolio version goal is complete.

Public release sources: [Python](https://www.python.org/downloads/), [FastMCP](https://pypi.org/pypi/fastmcp/json), [DeepEval](https://pypi.org/pypi/deepeval/json), [DeepTeam](https://pypi.org/pypi/deepteam/json), [Starlette](https://pypi.org/pypi/starlette/json), [Multidict](https://pypi.org/pypi/multidict/json).
