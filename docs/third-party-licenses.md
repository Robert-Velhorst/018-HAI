# License & Third-Party Review

## Project license

The repository ships a `LICENSE` file at the root and in each service directory
(`backend/`, `frontend/`). Confirm the intended license is consistent across all
copies before distribution.

## Key third-party licenses (backend)

Permissive licenses compatible with typical commercial/self-hosted use:

| Dependency | License |
| --- | --- |
| `gin-gonic/gin` | MIT |
| `gorm.io/gorm`, `driver/postgres` | MIT |
| `IBM/sarama` | MIT |
| `google/uuid` | BSD-3-Clause |
| `swaggo/*` | MIT |
| `golang.org/x/*` | BSD-3-Clause |

## Frontend

Angular (MIT) and ng-zorro-antd (MIT).

## Local runtime images

| Runtime | License / boundary |
| --- | --- |
| Redpanda Community Edition | Business Source License 1.1. Free and source-available for HAI's self-hosted Kafka-compatible broker use; it must not be offered as a commercial streaming or queuing service. Each release converts to Apache 2.0 on its stated change date. |

Redpanda is a runtime image, not linked into HAI's Go or Angular binaries. The
exact image version and multi-architecture digest are pinned in Compose. Its
license is non-permissive and therefore requires explicit review rather than
being added to the MIT/BSD/Apache allowlist.

## Process

- Generate a full license inventory in CI, e.g.:

  ```bash
  go install github.com/google/go-licenses@latest
  go-licenses report ./... > docs/generated-licenses.txt
  ```

- Fail linked-code builds on any license outside an allowlist (MIT / BSD /
  Apache-2.0) pending explicit legal review. Review runtime-image licenses
  separately; the approved Redpanda BSL boundary above does not expand the
  linked-code allowlist.
- Re-run on every dependency change.

## Status

No copyleft (GPL/AGPL) linked dependency was identified in the reviewed set.
Redpanda's BSL runtime is explicitly non-permissive and source-available, not
open source under the OSI definition. A generated authoritative inventory via
`go-licenses` plus a container-license inventory is the recommended next step.
