# Retained Windows lifecycle and SMTP reconciliation

## Retained source decisions

The retained Windows start, stop, and maintenance-task manager scripts were read
in full and compared with the current versions. Keep current behavior:

- Startup checks protected environment availability and existing data before
  initialization, validates required settings, and checks installation ownership
  again after initialization before selecting Compose arguments. Host-worker
  changes remain after environment validation. Keep dashboard deep linking and
  the optional visible error pause instead of restoring the older entrypoint.
- Stop preserves Docker volumes and settings. Keep current temporary suppression
  and guaranteed restoration of ambient Compose overrides, including preservation
  of the actual Docker exit code across cleanup.
- Maintenance task management retains registration, unregistering, absence
  checks, and redacted logging. Keep current canceled-uninstall recovery, which
  permits synchronization bypass only for registration with immediate execution
  suppressed. Do not restore the older manager without that restriction.

The retained SMTP sender and its entire test file were also reviewed against the
current implementation. Preserve verified implicit TLS support on port 465,
mandatory STARTTLS elsewhere, exact password bytes, normalized ports, context
cancellation, bounded deadlines, redacted delivery stages, and recipient input
exclusion from message headers. The older port-465 rejection test describes an
obsolete implementation limitation, not a requirement to remove verified TLS.
The other retained configuration, mailbox, and encryption tests remain covered.

## SMTP resource repair

After SMTP client creation the sender deferred `Quit()` alone. A server can
reject QUIT while leaving the underlying connection open; the Go SMTP client
does not guarantee socket closure on that error path.

A local TCP SMTP fixture advertises no STARTTLS support and rejects QUIT. Before
the repair, the delivery method returned its redacted TLS failure but the fixture
timed out waiting for socket EOF. The sender now also defers `Close()`, after the
QUIT attempt, without changing the delivery result. An accepted-QUIT positive
control covers the ordinary cleanup path. No reset message or credentials are
submitted by either fixture.

## Verification and boundaries

These Windows checks passed:

```text
test-windows-lifecycle-production-source.ps1 -MockedStop -MockedOwnership
test-windows-start-env-validation.ps1
test-windows-uninstall-maintenance-recovery.ps1
```

They parse production sources, execute isolated helper fixtures, and replace
Docker/task operations with mocks. The mocked stop success message is not proof
that the real installation was stopped. No actual container, installed worker,
scheduled task, or protected installation settings were changed.

The SMTP regression failed before the socket-close repair. After repair the full
IDP services, authentication, users, and router packages passed, including both
rejected-QUIT and accepted-QUIT local socket controls. These are fixture tests,
not proof of live email delivery or
real Windows install/uninstall acceptance. Original worktrees remain intact;
publication, CI, main-preservation checks, and local deletion remain separate.
