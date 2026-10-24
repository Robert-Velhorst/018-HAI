# ACL source package candidate

Prepare ACL 2.4.0 from the published Debian 2.4.0-1 source recipe with its normal compat-13 tooling. Publisher and Debian original source archives have the same SHA-256. Every input is pinned, including both published Debian patches: example LDLIBS linking and getfacl uninitialized-state handling. No test file is patched.

The hosted workflow builds the binary packages with GCC 16.2 / Python 3.15.1 as UID 10001 in a disposable, network-disabled SDK with a read-only root and bounded writable build storage. Only reviewed public inputs and a dedicated output directory are mounted. Ordinary dpkg-buildpackage retains normal dependencies and upstream checks; nocheck/stage1 profiles, root-guard bypasses and test exclusions are absent. Root/NFS cases may skip or be expected failures in this profile; these remain unqualified and keep the qualification gate failed.

The artifact retains packages, package metadata, build log, complete built source and all original source inputs. Original test bytes and source changes outside the two reviewed Debian patch paths are checked even on failure. Candidate packages are never installed in the operator machine, server or application images by this workflow. Package generation, complete upstream case qualification, ABI/CLI/dependency compatibility and actual image scan are separate requirements. All image findings remain open until the real package replacement qualifies.

ACL 2.4 adds a public API that collides with names in older GNU tar source builds. Debian bug 1141146 documents its tar source-build fix. This workflow does not build tar or modify installed runtime binaries. Review actual tar/library compatibility before integration. A matching package name alone proves no compatibility or remediation.

Sources: https://download.savannah.nongnu.org/releases/acl/ ; https://deb.debian.org/debian/pool/main/a/acl/acl_2.4.0-1.dsc ; https://security-tracker.debian.org/tracker/CVE-2026-54369 ; https://bugs.debian.org/1141146 . Checksum identity is verified; detached PGP authentication is not claimed.
