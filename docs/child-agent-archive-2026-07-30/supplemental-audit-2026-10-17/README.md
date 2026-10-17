# Supplemental transcript audit - 2026-10-17

The supplemental July 30 HAI transcript archive was re-audited with the checked-in transcript auditor from PR branch commit `aacc0d412931cf6785c41f270ed4dc93d489f9c6` (`scripts/audit-child-agent-transcripts.ps1`, blob `a70ceb4f2fd712798ee2e51448e4f73989536d52`; shared record parser blob `7c41dbc58e0e311831fb57c12cc26c95ce1a51af`). The scan was read-only and wrote only the three adjacent artifacts.

- 13 transcript files; 17,559,729,527 logical bytes scanned.
- 11 unique child IDs; two child IDs have duplicate files.
- Eight aborted, five unattributed, zero completed or observed-completed.
- Two oversized records were skipped, so `full_file_scan` is false.
- All 13 remain retained; there are zero cleanup candidates. Candidate hashing was not requested.
- No transcript content was copied into the report; the manifest records redacted metadata and hashes of terminal messages. A basic credential-pattern scan found no obvious secret matches in the generated artifacts.

The duplicate files are not confirmed byte-identical, and the oversized records remain unresolved. Do not delete or treat this archive as integrated based on this audit alone.

Artifact SHA-256:
- Manifest: `a03b17a91f71dd81698c1f928a5c00e84e5bdddd19c35fe21ac4232fbf7b0d52`
- Summary: `1735bd0c915f7603d6a28aabfdc39dac3f75bc09618c5ff6e9a024c4ed6078dc`
- Final-report index: `0becf60adb6b85dc84db4cb55629e6ff5be4e7eba7b18b628efacad5cfd87c2e`