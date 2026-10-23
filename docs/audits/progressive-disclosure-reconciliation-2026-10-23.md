# Progressive disclosure and native CI coverage

## Original source disposition

The original working tree still contains the disclosure template and twelve
rendered component scenarios. Their complete deltas retain all original content
except a deep-link `data-hai-section` marker and test-isolation/marker assertions.
All twelve scenario names and their original assertions remain.

The implementation `.ts` file was already absent in the frozen original status
inventory. Its historical original-HEAD Git blob is a much older implementation
that lacks visibility, effective-open state and accessible control identifiers
used by the retained template. That historical blob is not a present source
copy and must not be blindly restored over the current implementation. No old
working-tree absence is staged or published as an intentional deletion here.

The current implementation retains saved section state and explicit toggling,
and adds per-module subscriptions, effective-open notifications, distinct
control/panel IDs, focus restoration, registered deep-link handling and teardown.
No production component/template behavior was changed in this review.

## Independent template regression

Two native Angular-template-parser contracts now check the shared disclosure's
button type, matching deep-link marker, expanded state, conditional controls
reference, panel identity, region role and label relationship. Negative fixtures
remove the marker, retain a stale controls reference or change the panel label;
each mutation is rejected. These parser tests do not replace rendered DOM,
keyboard or assistive-technology acceptance.

## Shared local and CI command

The old CI native command carried a separate, smaller copied list of tests.
The frontend now declares that list once as `test:native`; ordinary `npm test`
calls it before Angular tests, and CI calls the same command before its separate
headless Chrome step. The resource-parser regressions remain a separate gate.
No dependency is changed and Chrome is not deliberately run twice.

The complete local native run passed 262 tests with zero failures or skips.
An earlier attempt failed two fixture tests when their temporary directory did
not exist while D: had no free space; that failed attempt is not treated as a
pass. An inactive, task-owned Go build cache was cleared with `go clean -cache`,
reclaiming 1,907,359,744 bytes. The native tests were repeated only after verifying
free space and creating/checking their temporary root. Source, audit reports,
dependencies, archives and databases were retained.

For the already published commit `b3d2b00957aaa573bf99e25ee48b9abeb6b6a915`,
CI run `40018740909` has a successful frontend job with 2,283 headless Chrome
unit tests. The disclosure template/spec content is unchanged from that commit.
That result is not a new browser run of subsequent local CI/script changes,
live authentication or an operator's visual/keyboard acceptance. The new shared
native CI wiring still needs its own published-head execution.

The remaining missing original files and other source/archive variants require
separate review. This checkpoint does not authorize deleting any original
checkout, transcript archive, database or worktree.
