# Docling and Pillow security cohort

## Compatibility decision

[Pillow 13.0.0 release notes](https://pillow.readthedocs.io/en/stable/releasenotes/13.0.0.html)
document security fixes and backwards-incompatible changes. Simply replacing
Pillow 12.3.0 in the old Docling lock is invalid: published Docling core/model
metadata constrained Pillow below 13.

Official current package metadata permits Pillow 13. Resolve this cohort
together, without dependency overrides:

| Package | Previous | Updated |
| --- | --- | --- |
| docling | 2.139.0 | 2.142.0 |
| docling-slim | 2.139.0 | 2.142.0 |
| docling-core | 2.101.1 | 2.104.0 |
| docling-ibm-models | 4.0.3 | 4.0.4 |
| docling-parse | 7.22.2 | 7.23.0 |
| doclang | 0.7.3 | 0.8.0 |
| pillow | 12.3.0 | 13.0.0 |

The Doclang update follows Docling core's dependency requirements. Regenerated
both manifest outputs using the existing uv Linux/Python 3.12 resolver inputs,
targeted package upgrades, generated hashes and `--no-build`. Kept the official
PyTorch CPU index, torch 2.14.1+cpu and torchvision 0.29.1+cpu. No CUDA package
or unrelated package-version change was introduced. The certifi security pin
from the preceding update remains 2026.10.19.

The actual image installs Docling directly rather than its CI lock. Update its
declared Docling version from 2.140.0 to 2.142.0 and explicitly constrain Pillow
13.0.0 there too. This does not claim all runtime transitive versions equal the
Python 3.12 CI lock: image installation/build acceptance remains required.

## Verification

- Resolver completed successfully without building packages or overriding
  declared dependency bounds. Only the seven package versions above changed.
- Verified all 83 Pillow distribution hashes in the generated lock against
  [official release metadata](https://pypi.org/pypi/Pillow/13.0.0/json).
- Six runner policy tests pass, covering manifest parity, the fixed cohort,
  actual Docker install pins and CPU-only PyTorch selection.
- In an isolated Pillow 13 environment, actual native PNG, JPEG and TIFF
  save/load checks pass. The existing three lightweight Docling boundary tests
  also pass; no existing Python environment was modified.
- Added an actual offline HTML-to-Markdown conversion test using the installed
  Docling package. It skips locally because Docling is not installed; the
  dependency-installed CI matrix now sets a mandatory-package flag, so missing
  Docling/Pillow fails rather than skipping there.

These checks are not hostile-document fuzzing, full-format conversion
acceptance or proof that an existing deployed image was rebuilt. No alerts are
dismissed manually. Full exact-head CI must pass before merge. Original source
worktrees, transcripts and databases remain intact.
