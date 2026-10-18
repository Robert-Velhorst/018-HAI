# Supplemental transcript audit - 2026-10-17

The supplemental July 30 HAI transcript archive was re-audited with the checked-in transcript auditor from PR branch commit `aacc0d412931cf6785c41f270ed4dc93d489f9c6` (`scripts/audit-child-agent-transcripts.ps1`, blob `a70ceb4f2fd712798ee2e51448e4f73989536d52`; shared record parser blob `7c41dbc58e0e311831fb57c12cc26c95ce1a51af`). The scan was read-only and wrote only the three adjacent artifacts.

- 13 transcript files; 17,559,729,527 logical bytes scanned.
- 11 unique child IDs; two child IDs have duplicate files.
- Eight aborted, five unattributed, zero completed or observed-completed.
- Two oversized records were skipped, so `full_file_scan` is false.
- All 13 remain retained; there are zero cleanup candidates. Candidate hashing was not requested.
- No transcript content was copied into the report; the manifest records redacted metadata and hashes of terminal messages. A basic credential-pattern scan found no obvious secret matches in the generated artifacts.

The duplicate files are not confirmed byte-identical, and the oversized records remain unresolved. Do not delete or treat this archive as integrated based on this audit alone.

## Public export

This is a sanitized historical audit export. Session, turn, worker, and
transcript labels are aliases; machine paths are replaced with local-path
labels. The original reports remain recoverable in retained local evidence.
The public manifest intentionally uses alias column names and must not be
supplied to source-verification or deletion tooling. Counts, dispositions,
and content hashes are preserved; source files remain retained.

SHA-256 values below cover the sanitized export, not the original artifacts:
- Manifest: `e9dd7f1e81684610c0040484d7168562ca7e93393af11645d5c89ef35c73caad`
- Summary: `a577397bb970ead2da5ab4bb66313573167cf9d1cfa096e1a0d55e6d2160ade3`
- Final-report index: `4c23f28a908fb4977ebc345b073fd530ce949a767ba4c7074aba0cf4ef591129`
