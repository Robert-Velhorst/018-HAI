# Local runtime-verification checkout reconciliation

Reviewed remote main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Candidate checkout head: `774ecb127a1a3f6ee10d3263c3f3a3d924d3fdec`.

## Remote preservation evidence

A fresh GitHub comparison reports divergence, not ancestry: main has 173 unique
commits and the candidate has three, with common base
`1e18f90cfccf525781c4e3c7c819a26fceba2edb`. The candidate's three commits update
Node 24 tooling twice, then align Node tooling to 26.12.0. Their comparison
against the common base touches 13 paths, including CI, Dockerfiles, package
manifests/locks, runtime tests and documentation. This is not a claim that only
13 files differ between the complete main and candidate trees.

Current main uses Node 26.12.1, npm 12.2.1 and digest-pinned frontend/Promptfoo
base images. The frontend, browser-test and Promptfoo engine declarations agree
with the newer minimum. E2E direct dependency declarations retain the candidate's
Playwright, Node types and native/JavaScript TypeScript alias choices. Current
runtime tests reject Node 26.12.0. These are targeted successor observations,
not a complete transitive-lockfile or unique-history preservation proof.

The current main README still names 26.12.0 in its two toolchain instructions.
The already-prepared PR version corrects both to 26.12.1; no duplicate README
patch was added by this review. Until publication and main integration, that
correction is not main-preserved.

## Eight existing native contracts

The prepared build wrapper, its existing test file, imported theme-manifest
module, package manifest and Angular configuration were compared to the local
test inputs by normalized Git blob identity. All five match the prepared tree.
The test file and package manifest do not match current main; the result below
is therefore explicitly prepared-source evidence, not current-main acceptance.

Running the existing `frontend/scripts/build-with-limits.test.mjs` suite under
Node 26.12.1 with one test worker yields eight passes, zero failures and zero
skips. It covers default/allowed worker limits, environment immutability,
invalid/contradictory settings, literal watch arguments, build exit/signal/error
handling, guarded scripts, strict bundle-budget configuration and supported
runtime policy. Launch functions are test doubles; no Angular compilation,
Docker image build, browser or external runtime is started by this suite.

The result does not waive the separately failing Promptfoo dependency audit,
establish a production build, or prove full historical dependency equivalence.

## Bounded local cleanup

Only rebuildable generated content in this temporary checkout was removed.
Native Angular cache cleanup removed three cache files (3,609,927 logical bytes).
The separate application dependency tree contained 485 installed packages,
36,453 files and 272,752,226 logical bytes; the browser-test dependency tree
contained nine installed packages, 1,020 files and 76,620,822 logical bytes.

For each dependency tree, installed versions, resolved registry locations and
integrities matched its retained lockfile. The trees were ignored, not tracked,
had no reparse descendants, local dependency links, nested Git projects or
private `.env` files, and passed active-consumer/mount checks. Scoped native
PowerShell removal was used, with protected configuration/source hashes checked
afterward. No source checkout, Git history, database or transcript was deleted.

The candidate remains a preservation-required checkout. A clean working tree
and newer runtime versions alone do not clear its three unique commits or all
historical lock changes. Whole-checkout deletion eligibility remains false.
