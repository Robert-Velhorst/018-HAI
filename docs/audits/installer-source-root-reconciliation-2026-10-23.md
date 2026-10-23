# Installer source-root reconciliation

## Retained source review

Reviewed the saved model-audit versions of these files in full, then all differences
against current code:

- `scripts/windows-installer-payload-selection.ps1`
- `scripts/test-windows-installer-source-selection.ps1`
- `installer/windows/Run-HAI-OpenClawMaintenance.ps1`

Preserve tracked-source enumeration, the narrow untracked installer/worker
allowlists and case-insensitive deduplication. Keep newer exclusions for secrets,
credentials, databases, archives and runtime state, while retaining the explicit
versioned schema-migration exception and `.env.example` template.

Retain per-file provenance hashes/sizes, bounded compiler diagnostics and rejection
of absolute, traversing and descendant-reparse source paths. Keep the newer Windows
worker validation helper and bounded maintenance lock wait rather than restoring
the old immediate-failure lock path or unvalidated executable invocation. Validation
mode stays non-executing; batch size, environment restoration and mutex cleanup
remain represented.

## Source-root ancestry correction

The current source-file checker walked only below its supplied root. A Windows
junction used as that root, or above that root, was followed before this walk
began. It could therefore describe selected bytes without enforcing its intended
no-reparse boundary across the source path.

The checker now inspects the root and every lexical ancestor as regular directories
before selecting the file. Existing descendant checks remain unchanged. This is a
pre-use path check, not a race-free filesystem handle or protection against a
concurrent privileged filesystem mutation.

## Verification and limits

Both root/ancestor junction cases were accepted in an observed Windows regression
before the fix. Afterward, the complete Windows source-selection script passes,
including these rejection cases, a regular-root positive control, exact hash/size
provenance, sensitive-file exclusions and the actual current Git source inventory.
The fixture creates only a fresh temporary directory/junction, removes the exact
junction entry without recursively deleting its target, then removes its owned
temporary directory after checking its location and generated name.

No compiler, installer, executable worker, scheduled task, Docker service or live
provider is run by this test. Installer packaging, signing and operator acceptance
remain separate release gates. The original recovery files remain retained;
semantic review and PR publication do not make a whole worktree disposable.
