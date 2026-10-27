# Current crypto source port and original SDK qualification

The latest public Trivy release, v0.76.1, declares
`golang.org/x/crypto v0.57.0`. The Go module proxy currently publishes
v0.58.0. This candidate starts from the complete immutable Trivy source at
`e713f67f7d54decdcd9b3132c9751833dcced698` and changes that one direct
declaration. Go 1.27.3 generates the resulting dependency graph and go.sum
normally on a GitHub-hosted runner.

The source reader binds all 3,412 publisher payloads to the pinned Git tree
before preparing the declaration. After normal resolution it requires every
code, original case, assertion, helper, fixture and other non-dependency
payload to remain identical. Only go.mod and go.sum may change. It checks
the selected crypto version, checksum database, actual Go SDK version and
absence of newly introduced module replacements. Complete graph, source
identity, original/prepared/resolved manifests, checksums and logs are retained.

Normal resolution is followed by all eight original SDK job definitions:
three-platform unit and build matrices, integration, Kubernetes, module, VM,
end-to-end and workflow lint (12 platform jobs). Every original SDK command
and platform matrix is retained. Current stable qualification tools are
pinned, and original fixture downloads remain active without a shared cache.
Runner capacity is measured without deleting installed tools or pruning images.

Each SDK job adopts the successful same-run manifests and creates an
ephemeral, never-pushed Git snapshot in its own hosted public-source checkout.
This gives the original clean-tree checks a real port baseline. Complete
source and resolved input identities are checked again after SDK execution.
Windows uses canonical LF checkout and its normal Git representation for
symlink-target files; original publisher case/helper/fixture bytes remain protected.

The configured SDK jobs still require successful execution. Runtime adoption
also requires actual binary/native identity, the complete saved final image,
original HAI container cases, health and the raw all-package/all-severity gate. Other transitive modules have not
been proven to be their highest public stable versions. No deployment or
operator database/data operation is part of this source candidate.

GO-2026-5932 concerns the unmaintained x/crypto/openpgp packages. Its current
Go advisory contains no fixed-version event. The proposed version upgrade
does not claim to fix it. The inspected publisher release binary already
contains maintained ProtonMail crypto functions and has no matching legacy
openpgp names in its Go function-name table; those metadata observations
are not a complete vulnerability/reachability verdict. The existing raw
finding remains fatal. No advisory, source, original case or scanner
exclusion, dismissal, waiver or replacement is introduced.

References:

- [Publisher release](https://github.com/aquasecurity/trivy/releases/tag/v0.76.1)
- [Immutable original SDK workflow](https://github.com/aquasecurity/trivy/blob/e713f67f7d54decdcd9b3132c9751833dcced698/.github/workflows/test.yaml)
- [Current crypto metadata](https://proxy.golang.org/golang.org/x/crypto/@latest)
- [Current Go releases](https://go.dev/dl/?mode=json)
- [Go advisory](https://vuln.go.dev/ID/GO-2026-5932.json)
