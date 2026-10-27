# Adopt the qualified Mini-SWE Ubuntu recipe

The standalone Mini-SWE Ubuntu candidate at `a7672f3` passed its original
application cases, unchanged startup contract and all-severity image scan.
The complete saved-image artifact was independently reconciled: 22 layer
hashes, 78 native extensions, 83 locked distributions and 116 OS package
versions match, with zero raw or modified findings. This is scoped image
evidence; it does not establish complete application or SDK security.

This integration makes the normal Mini-SWE Dockerfile byte-identical to that
complete Ubuntu 26.10 recipe and retains the later `7fca1b2` diff-read/baseline
integrity change. Compose already supplies the required native source and
source-bound wheel contexts. No locally prepared base image is needed.

Normal CI now binds the complete current source tree and the exact default
recipe, retains current native exports, inventories real providers and
distributions, and runs every original container case. The existing exact-image
scanner still inventories all packages and treats every finding as fatal.
Mini-SWE selects its explicit Ubuntu policy; all other distribution policies
are preserved. The complete current default image is retained even when a gate
fails, so independent layer/binary/package reconstruction remains possible.

The five original cases, fixtures, assertions, helpers, canonical locks, model
command, production application source and distribution-policy scripts are
unchanged relative to `7fca1b2`. Four additional security cases and the current
LiteLLM SDK expectation update remain separate unapplied approval proposals.

The old standalone image is not promoted to this new source or default path.
The new normal CI job, actual native/image bytes and current source must be
independently qualified. Highest-stable LiteLLM/native crate work, full vendor
and platform suites, agent process/filesystem/network isolation, CodeQL 41 and
live deployment/data acceptance remain open. No advisory/source exclusion,
weaker fallback, merge, manual deployment or operator-data access is included.
