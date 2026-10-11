# Windows Installer Production Audit - 2026-09-30

Verdict: one reproducible payload-validation defect fixed; production release readiness is NOT established.
Observed checkout HEAD: `e07b9daeb3ba2630ecbe12a64d948f791203c241` in an extensively dirty worktree. Existing edits were preserved. No descendants, commits, pushes, publishing, installer execution, live Docker, registry, scheduled-task or service changes, download execution, or effective file-deletion operations were performed. Synthetic fixtures were written/replaced only in Temp and retained.

## Scoped Fix And Red/Green Proof

- `installer/windows/Hai-WindowsExecutable.ps1:33`: reject PE32+ optional headers smaller than their 112-byte fixed fields.
- `installer/windows/Hai-WindowsExecutable.ps1:43`: reject declared data directories exceeding the optional-header size, using widened arithmetic for the maximum UInt32 count.
- `scripts/test-windows-installer-payload-validation.ps1:53`: positive checks for exactly fitting directories and a complete fixed header with zero directories.
- `scripts/test-windows-installer-payload-validation.ps1:80`: regressions for a 64-byte header with a consistently relocated section table, 17 entries where only 16 fit, and UInt32.MaxValue entries. Before the fix, all three fixtures were accepted; the regression suite exited 1. After the fix, all original repro files are rejected, with 3 accepted header shapes and 12 rejected payload states.
- Layout checked against [Microsoft's PE specification](https://learn.microsoft.com/en-us/windows/win32/debug/pe-format#optional-header-image-only): PE32+ standard fields are 24 bytes, Windows-specific fields 88 bytes, and data-directory records 8 bytes. This remains structural validation, NOT proof of loadability, publisher identity, or a valid Authenticode signature.

## Verification Actually Run

| Check | Actual result / boundary |
| --- | --- |
| Existing source-selection test | PASS on Windows PowerShell 5.1.26100.9549 and PowerShell 7.6.5; release payload not regenerated. |
| Existing payload-validation test | PASS on both engines; exact validated-byte hash check plus positive/negative fixtures. |
| Existing update-promotion test | PASS on both engines: total mutex/process wait budget, substituted valid bytes, locked-file timeout, retry, malformed pending payload, user-data canary. No real worker executed. |
| Build transaction failure injection | Six PASS cases on both engines: success, payload backup failure, manifest backup failure, payload promotion failure, manifest promotion failure, incomplete payload rollback retaining its backup. Actual build transaction code executed with in-memory filesystem-command doubles, NOT real filesystem failure injection. |
| Existing recovery-contract test | PASS on PowerShell 7: valid/invalid manifests, checksums, archive paths, safety-state documents, and restore-source assertions. NOT a real backup/restore drill. |
| Installer static contract prefix | PASS on PowerShell 7 through the boundary before maintenance migration/launcher tests; includes syntax/source contracts and the 14-case version test. Full contract suite NOT run. |
| Default release-build preflight | Expected dirty-worktree refusal with `-SkipCompile`, before staging/toolchain execution; existing Setup and manifest hashes unchanged. |
| Whitespace check | Scoped `git diff --check` passed. |

Cleanup boundary: test `Remove-Item` calls were intercepted to retain fixtures. During promotion, a Temp-only read lock prevented production code from deleting its synthetic recovery copy, exercising the existing nonfatal cleanup-warning branch. Thus successful backup deletion is deliberately unverified. Recovery-test final directory deletion alone was suppressed in memory. Promotion still used the actual file replacement path on Temp fixtures; application data/volumes were not touched.

## Release Gaps / Parent Handoff

1. **Signing is not enforced in the reviewed installer release path.** Build completion invokes the compiler and checks output existence (`scripts/build-windows-installer.ps1:265`); `installer/windows/HAI.iss:1` has no SignTool/SignedUninstaller configuration. Worker validation/promotion (`installer/windows/Hai-InstallerSupport.ps1:172`) and launch (`installer/windows/Run-HAI-OpenClawMaintenance.ps1:44`) verify PE structure, not trusted publisher identity. No signing identity, certificate provisioning, or trust policy was invented or changed. The OpenClaw publisher-pin setting is not evidence that Setup or the bundled HAI worker is signed.
2. **Existing release is stale and unsigned.** Read-only `Get-AuthenticodeSignature` returned `NotSigned` for `installer/release/HAI-Setup-541b870.exe`; SHA-256 `E16EC572F3B16B8A7B968FFB5AAAA0A78785FF8F0E8ED840F70E110B6C3D990D`. Its adjacent manifest records commit `541b87042b2b5a4af7d6d0ff09c7976a46a82b44`, not current HEAD. No new installer was compiled; this artifact contains no demonstrated fix from this audit.
3. **Rollback/data coverage is bounded.** Build transaction rollback was tested with doubles; worker replacement with synthetic files. No disk-full/power-loss/Inno Setup rollback, interrupted upgrade, native uninstall, retained real environment/data/media/database volumes, real database restore, or publisher-chain/revocation/timestamp acceptance was tested. The worker backup is normally removed after successful replacement; that is not a whole-application/database rollback guarantee.
4. **Ownership respected.** Frontend, backend source, documentation and parent acceptance/CI scripts were not edited. Maintenance migration/launcher/task-scheduler and broader runtime acceptance suites were not run in this bounded audit. Parent must establish approved clean-revision packaging, signing enforcement and native acceptance before calling the installer production-ready.

## Retained Proof

Temp root: `C:\Users\NO\AppData\Local\Temp\hai-installer-audit-20260930-d3d0261b746943e49174ecf8138b11c1`.
- Repro/failure: `header-repro-before.log`, `directory-repro-before.log`, `regression-red.log`; original synthetic `.exe` inputs retained and never executed.
- Final Windows PowerShell proof: `tests-powershell-5.1.26100.9549-3fabecf265154a7e94c5f8adf7f2dff5.log`.
- Final PowerShell 7 proof: `tests-powershell-7.6.5-1f74bed1af9a412f82cf2541e1cb8341.log`.
- Other proof: `static-and-recovery.log`, `rollback.log`, `dirty-release-preflight.log`, `signature-and-state.log`, `pre-edit-hashes.log`; reusable Temp runners `retained-installer-tests.ps1` and `rollback-probe.ps1`.
- Failed audit-harness attempt retained as `tests-powershell-5.1.26100.9549-be6a836762364eeab691555dc3d4546d.log`: retention-variable scope error, corrected in the Temp wrapper before the final successful run. `powershell51.log` actually records PowerShell 7.6.5; use the version-labelled final logs above.

Capabilities used: direct current source, PowerShell parser, existing synthetic tests, isolated Temp probes, read-only Git/hash/Authenticode inspection, Microsoft primary specification. Diagnosis/validation skills were read; descendant-based diff-review workflow was inapplicable to the explicit no-descendants/current-worktree scope. Memory supplied only historical context, not current release evidence. No external connector or live-provider access was needed.
