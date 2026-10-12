# HAI DeepSeek Harness Bridge

**Execution is currently hard-disabled.** This package is not yet an operational Windows worker: its production startup, lease, execution, and completion entry points return an isolation-unavailable error. Setting `HAI_HOST_RUNTIME_BRIDGE_ENABLED`, `DEEPSEEK_HARNESS_ENABLED`, or `DEEPSEEK_HARNESS_EXECUTION_ENABLED` does not enable it. The supervisor code and its tests are implementation work, not proof that HAI can safely launch DeepSeek Harness.

Execution must remain disabled until HAI has an authenticated, peer-verified bridge channel; a verified OS-enforced least-privilege sandbox; a server-to-Windows start/cancellation protocol with explicit ordering and recovery semantics; and native Windows integration plus operator acceptance evidence.

## Process supervision boundary

The Windows Job Object is **process-tree lifecycle containment**, not an operating-system sandbox. It lets the bridge observe and terminate processes assigned to the job. It does not restrict filesystem, network, registry, or credential access. DeepSeek Harness and its native Windows child processes run with the permissions of the Windows account running the bridge. The bridge's workspace checks, explicit child environment, inherited-handle list, approval lease, and Job Object do not reduce that account's OS privileges.

The Windows supervisor implementation creates a process suspended and assigns it to a kill-on-close Job Object during process creation; its unit tests cover lease revalidation and fail-closed termination. This code is not reachable from the disabled production bridge entry points. A Windows Job Object is process-tree lifecycle containment only, not a security sandbox.

Stdout and stderr are captured with fixed size limits. A pipe-copy error or a pipe that does not drain before the bounded wait deadline produces a failed completion; intentional local pipe closure after that deadline remains an explicit incomplete-capture failure.

While execution is running, lease confirmation is attempted every two seconds and each confirmation has a two-second deadline. Failure to confirm cancels the execution and triggers whole-job termination. Scheduling and OS process termination add time beyond the confirmation deadline itself.

## WSL and plugin limits

The bridge rejects detectable direct WSL entry points, WSL UNC workspace/state paths, forwarded WSL-related environment variables, and a bridge environment that declares WSL integration. These checks only inspect the bridge environment and configured executable, paths, and environment allowlist.

They do **not** inspect DeepSeek Harness plugin configuration or runtime behavior. In particular, if DeepSeek Harness invokes `wsl.exe` through a plugin or a native wrapper, this bridge does not block that invocation and cannot claim that the resulting Linux task is contained by the Windows Job Object. Windows Job Objects are not a Linux process supervisor. Treat any plugin capable of launching WSL as outside this containment guarantee.

## Verification

The package includes platform-independent supervisor tests and Windows-only process-tree and exit-code integration fixtures. Linux tests and Windows cross-compilation do not prove Windows Job Object behavior; run the Windows integration tests on a native supported Windows host before relying on that behavior operationally.
