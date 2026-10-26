# Mini-SWE required PyYAML native feature

The prior qualified image contained PyYAML 6.0.3 Python files without a YAML
native extension. This did not establish complete native SDK coverage. A prior
production comparison has not established a regression.

Both normal and explicit Ubuntu recipes now install the signed Ubuntu LibYAML
development package and set upstream's required Cython/LibYAML build options.
The regular isolated PEP 517 build and all 83 runtime pins/hashes remain intact.
Compiler/link failures are fatal instead of allowing PyYAML's Python fallback.
Final runtime dependencies are still derived from every actual linked library.

A separate disposable SDK stage adds pytest 9.1.1; production retains the original
runtime-only graph and removes pip/bootstrap artifacts as before. Its source
input is the complete PyYAML 6.0.3 publisher archive, pinned to commit
`49790e73684bebad1df05ef8d828fa12f685bffb` and SHA-256
`9a1805b064ab6973ed8903ef46ee9a67e59a982c637f410c6561d81a4387bd8a`.
All 641 publisher payloads, including 598 test/fixture files, are hash-bound.

Dedicated hosted CI checks the actual installed extension and linked LibYAML,
runs upstream's unchanged default pytest collection with the native extension
required, and runs its unchanged existing wheel smoke script. Original source
hashes are checked before and after execution. Logs, JUnit, installation reports,
actual versions and extension hashes are retained even when the suite fails.
No application case, original assertion, fixture or collection helper is changed.
Original upstream default collection limits remain; full platform/large-file
acceptance is not inferred. The normal CI image job separately tests all five
original Mini-SWE cases, health and the complete all-severity image inventory.

Local work was source/archive/hash review and Python AST checks only. No native
build, application/vendor test or scanner ran locally. Hosted results are required
and are not inferred from preparation. LiteLLM's latest version port, original
SDK version-guard exception, four new security cases, complete native crate
currency, other HAI runners, agent isolation and live deployment/data acceptance
remain separate open gates. This draft is not approval to merge or deploy.
