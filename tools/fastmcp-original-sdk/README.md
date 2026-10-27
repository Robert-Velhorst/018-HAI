# FastMCP 4.1.4 original SDK evidence

The HAI bridge's `test_app.py` is an application contract, not the complete FastMCP publisher SDK. This independent hosted workflow checks out the immutable 4.1.4 publisher commit and calls its original setup and pytest composite actions without modifying their code, marker expressions, timeout settings, assertions, fixtures or helper files.

All eight original locked Linux/Windows unit-plus-serial matrix entries are retained. The original Python 3.10 conformance entry and an additional current Python 3.15 conformance entry use the original conformance action. Historical interpreters are isolated SDK compatibility checks; HAI's Python 3.15.1 runtime and current 71-distribution graph are unchanged.

The guard hashes all 1,854 original publisher Git payloads, including cases, helper actions, manifests and locks, before and after execution. The publisher's locked resolution, release-age policy and existing case selectors remain intact. No prerelease allowance, override, lower-resolution mode, original case edit, private credential or external-account mutation is supplied.

This is an incremental coverage addition. The required gates in `spec.json` remain open: private integration, lowest-direct policy, package smoke that allows prereleases, static and upgrade checks, the schema dataset, and downstream consumers. The completeness job fails while any required coverage remains absent; public matrix success cannot establish complete SDK qualification. No older runtime-image proof transfers to a new graph or SDK.

The separate schema job preserves the publisher's Python 3.12 setup and exact schema-test command. The original fixture selects openapi-directory commit `f7207cf0`; all 4,151 dataset payloads and all 1,854 publisher payloads must reconcile after execution. Per-provider results, raw error buckets, failure diagnostics and unchanged baseline caps are retained. A passing original case is not a claim that all raw error buckets are zero. Its coverage requirement is cleared only if the complete schema job succeeds; every other missing SDK gate remains fatal.
