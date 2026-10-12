# Reviewed Local-Analysis Snapshots

This directory is the read-only input mount for the optional `secret-scan` and
`sbom-inventory` Compose profiles. Do not put a live working tree or any source
that has not been explicitly access-approved for this purpose here. A reviewed
snapshot can contain a potential secret because detecting that exposure is the
scanner's job; it must remain on the local device and is never returned by HAI.

Each configured snapshot must be a direct child directory with a simple name
such as `release-review-2026-07-21`. The same name must appear in
`HAI_GITLEAKS_WORKSPACES` and/or `HAI_SYFT_WORKSPACES`. The runners do not
accept arbitrary paths, source uploads, commands, scanner rules, SBOM exports,
or report destinations.

The runners return aggregate, redacted evidence only. Gitleaks can return
finding/rule counts; Syft can return package/ecosystem counts. Neither exposes
source content, filesystem paths, or package-level metadata. Review any result
in the original workspace using the approved local process; this mount must not
be treated as an evidence archive or a place to remediate source material.
