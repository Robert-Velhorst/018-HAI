# OR-Tools / Protobuf 7 source compatibility profile

The current public OR-Tools release, 9.15.6755 / source tag v9.15, constrains
Python Protobuf to >=6.33.1,<6.34 and fetches native Protobuf v33.1. The current
Protobuf release is native v36.2 / Python 7.36.2. Updating only a Python constraint
would leave the generated code and native engine bound to the older library.

This proposal derives explicitly modified `9.15.6755+hai.protobuf7.1` from
OR-Tools commit `551ad10d94835c99e5e1e684500d3db398c0e345`. Five exact source
edits select the SHA-256-bound Protobuf 36.2 source archive, identify the local
package version, require the matching Python runtime and migrate the removed
MathOpt FieldDescriptor.label access to the equivalent is_repeated property.
The solver mathematics, schemas,
application bounds and all existing scheduling cases remain unchanged.

The original OR-Tools Protobuf CMake patch adapts Zlib target propagation and
static-runtime defaults. Its intent is retained through three hash-bound edits
to the actual Protobuf 36.2 CMake file. No solver or Protobuf implementation is
replaced with a stub. The public source archive is bound to commit
`2c74169b34066ceb8ddb6b882fcb3fb32d737a55` and SHA-256
`ef957adfc3ec0e2794031697e6a94bbc213103cead96c26a1fccb0dbdadd3404`.

`apply_source_port.py` uses only the standard library, validates every original
and resulting file digest before writing any target, and requires an explicitly
marked disposable SDK. It records the original/modified source identities in
HAI_PORT.json. It does not import or execute the engine, access an operator SDK,
connect a provider or open an application database.

Separate candidate inputs select Protobuf 7.36.2, CMake 4.4.4 and SWIG 4.5.1.
Public-only metadata resolution generates runtime/build locks before canonical
integration. These newer configuration/generation tools do not establish that
every native compiler or bundled library is current.

Keep this proposal draft. Native build compatibility, generated-code/runtime
identity, unchanged existing SDK/container cases, health and complete image scans
require actual hosted proof. Source integrity alone proves none of those outcomes.
No new application cases or local SDK/native/application/scanner runs are added.
Main merge, deployment and operator data changes remain separate.

Primary records:

- https://github.com/google/or-tools/releases/tag/v9.15
- https://github.com/protocolbuffers/protobuf/releases/tag/v36.2
- https://pypi.org/pypi/ortools/9.15.6755/json
- https://pypi.org/pypi/protobuf/7.36.2/json

## Integrated build/runtime path

The verified candidate runtime (9 packages) and build (14 packages) locks are
integrated into the canonical mirrors. The root requirements files declare
runtime prerequisites; the supported complete installation is the source-built
Docker/CI wheel. They no longer pretend the incompatible upstream wheel can
provide this Protobuf 7 profile.

The upstream OR_TOOLS_PATCH=6755 packaging input identifies the public release
build number, with the local port suffix retained. Earlier shallow-clone builds
reported the upstream fallback 9.15.9999; that is not a published release.

Compilation uses UID 10001. Original scheduling cases run without network as
UID 10001 in both build SDK and runtime image. Runtime imports assert matching
local package/module and Protobuf versions. The image inherits the unchanged
source-pinned OpenSSL 4.1.0 / SQLite 3.54.1 builder/profile, drops its installer
and bootstrap wheel, and receives the existing strict exact-image scan without
exclusions. An explicit Docker context allowlist excludes operator/secret files.
Other compiler/bundled/native/OS versions and real runtime compatibility remain
unqualified until their actual hosted evidence is inspected.
