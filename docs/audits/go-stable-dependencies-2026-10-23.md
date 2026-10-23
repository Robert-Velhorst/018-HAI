# HAI backend stable Go dependency refresh - 23 October 2026

Base main: `b4f4bc91206eb58be4d6f412c0829c498c9f4889`.

Updates Gin 1.12.0, pgx 5.11.0, Prometheus client 1.25.0, Redis client
9.23.0, Temporal API 1.64.0, Temporal SDK 1.50.0, and GORM PostgreSQL
driver 1.6.3. Refreshes the module graph and checksums with Go 1.27.3.
Tidy also recognizes the existing source's x/sync import as a direct dependency.

All 20 resulting direct module declarations match the official Go proxy's
current stable metadata in this dated audit. A subsequent `go get -u -t ./...`
refreshes the imported production and test dependency graph as well. Every
explicit direct and indirect go.mod declaration matches current proxy metadata
after tidy. The complete selected graph still contains 44 version differences
across 210 module records; none is an explicit go.mod declaration. Reachability
of those dependency metadata/test/tool requirements remains to be classified.
This does not establish that every transitive module, native library, database
or deployed image is current.

## Verification and limits

- Actual cached Go 1.27.3 Linux container, digest
  `sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c`.
- Temporary source, downloaded modules and build cache were confined to a
  bounded RAM filesystem. No additional SDK extraction or operator database.
- `go get` for the seven exact versions, then `go get -u -t ./...`, `go mod
  tidy`, `go mod verify`, and complete selected-module graph enumeration
  succeeded. Both intermediate and broad outputs passed checksum verification.
- Compilation, unit/race tests, migration compatibility and vulnerability
  checking remain subject to the existing hosted CI. No local test execution
  or live provider calls were performed for this dependency refresh.
- This draft depends on the Node/Angular/Promptfoo draft #330 and uses its branch
  as the review base. Its code is included in the combined validation head;
  neither draft has been merged into main.
- Deployment target remains Hetzner; no deployment or operator migration.

## pgx URI compatibility

The initial hosted dependency build passed vet, then the existing credential
preservation test failed. pgx 5.11's libpq-compatible URI parser treats '+'
literally, whereas Go's Values.Encode emits '+' for spaces. The constructor
now converts those space encodings to '%20'; encoded literal plus remains
'%2B'. Credentials remain escaped and verified TLS policy remains intact.

The existing regression retains all original cases and adds mixed spaces,
plus signs and percent sequences in user/database/socket fields. An isolated
Go 1.27.3 / pgx 5.11 probe copied the exact production constructor and SSL
policy bodies: five round-trip cases and a verified-TLS/error-redaction case
passed without making a database connection. The small probe uses a minimal
settings type and does not qualify the full infra package or hosted suite.

## Direct module metadata

| Module | Selected stable version |
| --- | --- |
| `github.com/IBM/sarama` | `v1.61.1` |
| `github.com/gin-gonic/gin` | `v1.12.0` |
| `github.com/google/uuid` | `v1.6.0` |
| `github.com/jackc/pgx/v5` | `v5.11.0` |
| `github.com/json-iterator/go` | `v1.1.12` |
| `github.com/prometheus/client_golang` | `v1.25.0` |
| `github.com/redis/go-redis/v9` | `v9.23.0` |
| `github.com/stretchr/testify` | `v1.12.1` |
| `github.com/swaggo/files` | `v1.0.1` |
| `github.com/swaggo/gin-swagger` | `v1.6.1` |
| `github.com/swaggo/swag` | `v1.16.6` |
| `go.temporal.io/api` | `v1.64.0` |
| `go.temporal.io/sdk` | `v1.50.0` |
| `golang.org/x/net` | `v0.61.0` |
| `golang.org/x/sync` | `v0.24.0` |
| `golang.org/x/sys` | `v0.49.0` |
| `golang.org/x/text` | `v0.43.0` |
| `google.golang.org/protobuf` | `v1.36.12` |
| `gorm.io/driver/postgres` | `v1.6.3` |
| `gorm.io/gorm` | `v1.31.2` |
