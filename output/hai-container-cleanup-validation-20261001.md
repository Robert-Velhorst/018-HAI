# HAI validation-container cleanup

User direction: leave Joyce Work Schedule and ShareT running; remove unused HAI test environments without deleting persistent data.

Pre-removal inspection on 2026-10-01:

| Container | Full identity | State | Persistent mounts | Database storage |
| --- | --- | --- | --- | --- |
| hai-codex-validation-local-pg-20260924 | e70c3d307fe6acb1957acf24610eeab2e12125ab86bf3c17c934d905eb29e84b | exited since 2026-09-26 | none | tmpfs, 512 MiB, already released on stop |
| hai-codex-validation-pg-20260924 | 068ae49f35670673a94a977cbf694cbd31b34bed3ea05bc413fa36f02ae84612 | exited since 2026-09-26 | none | tmpfs, 768 MiB, already released on stop |

Both have label `hai.validation.scope=codex-session`, restart policy `no`, PGDATA `/var/lib/postgresql/data`, and only socket/runtime-directory changes in the writable layer. Each writable layer measures 20,480 bytes. Removing these stopped container shells does not release running RAM or CPU; do not claim a performance percentage from this operation.

Removal must recheck full identity, name, stopped state, label, absence of persistent mounts, and tmpfs placement before proceeding. Use ordinary container removal only: no force, volume removal, image removal, or global prune. Preserve the stopped main HAI stack, all other projects, source files, and all data volumes.

The five volumes recorded in the earlier cleanup receipt at `C:/Users/NO/AppData/Local/Temp/hai-container-cleanup-72ec25b5b001486d84354e26743f37a1/receipt.json` were inspected and remain present.

Result: both full container identities were rechecked against all guards, removed without force or volume flags, and confirmed absent from the Docker inventory. Total additional writable-layer space removed: 40,960 bytes. No images, volumes, production containers, or other projects were removed.

Post-removal inventory: eight containers running. Four belong to Joyce Work Schedule, three to ShareT, and one is another project's active Cofounder test database. All were left untouched. HAI has no running Docker containers. Joyce's app, MySQL, and Redis report healthy; ShareT's app reports unhealthy while its tunnel and autoheal remain running. Running does not imply application health; this cleanup does not fix ShareT.

The previously running HAI native integration-test compile finished with exit 0 and no timeout before cleanup; no new tests, agents, or containers were started during this cleanup pass.
