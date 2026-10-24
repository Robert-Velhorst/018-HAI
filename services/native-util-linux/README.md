# Fixed util-linux source candidate

This workflow prepares util-linux 2.42.4 from its publisher archive, verified
against the publisher checksum and Debian's orig-source checksum. The source
file containing the CVE-2026-78408 fix is also bound to its release Git blob.
The referenced fix commit is present in v2.42.4 and absent from v2.42.3.

The source is compiled with GCC 16.2 and Python 3.15.1 in a disposable,
network-disabled, read-only-root SDK container as UID 10001. Only pinned public
inputs and a dedicated output directory are mounted. A writable executable
build tmpfs permits compiled configure probes and the existing native tests.
The operator machine and General-Server are not build targets.

The regular Linux configuration follows the installed Debian 2.41.5 feature
profile, explicitly requiring the library family and login. Its existing
disabled commands are supplied by other Debian packages; these are not a new
reduction of the installed util-linux package's feature set. Python bindings
were already excluded by that Debian packaging profile. No stage1, nocheck,
nodoc, noverity or test-exclusion profile is introduced. Unknown configure
options fail. The ordinary `make check` suite is run unchanged; skipped or
unparsed results do not qualify all cases. Logs and original source hashes
remain available when qualification fails.

The workflow retains a compiled installation prefix and test evidence. It does
not build or install replacement Debian packages. Debian's current source
packaging requires debhelper >=14.1~, compat 14 and a newer debputy sequence;
that prerequisite has not been bypassed. The nine installed package names exist
in the newer source metadata, but package-name availability is not ABI, CLI,
maintainer-script or dependency equivalence. Those must be established before
application-image integration. Source qualification does not clear the existing
image security gate or establish live mount/namespace behavior.

Primary references:

- https://www.kernel.org/pub/linux/utils/util-linux/v2.42/
- https://deb.debian.org/debian/pool/main/u/util-linux/util-linux_2.42.4-1.dsc
- https://security-tracker.debian.org/tracker/CVE-2026-78408
- https://github.com/util-linux/util-linux/commit/286dd3ff41526b582ef48830de239dffbaa61f90
