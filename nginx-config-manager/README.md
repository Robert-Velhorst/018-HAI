# automation-hub-nginxconfigmanager

Consumes durable automation events and materializes nginx route fragments.

The service uses a stable Kafka consumer group and commits offsets only after
the requested filesystem effect has either completed or reached a bounded,
persistent dead-letter state. Completion receipts are stored beneath
`NGINX_CONFIG_MANAGER_INBOX_DIR`, which must be on persistent storage. Replayed
events are skipped by their stable `eventId`; legacy envelopes fall back to the
Kafka topic/partition/offset identity.

Filesystem effects are independently convergent: identical rendered configs
are not rewritten, repeated deletes succeed without work, and a reload is
requested only when the materialized configuration actually changed. This
covers the crash window between applying an effect and persisting its receipt.

Required configuration:

- `KAFKA_BROKERS`
- `KAFKA_TOPIC`
- `CONFIG_DIR`
- `NGINX_CONFIG_MANAGER_GROUP_ID`
- `NGINX_CONFIG_MANAGER_INBOX_DIR`

The local Compose profile supplies a stable consumer group, a persistent inbox
inside the mounted nginx configuration directory, five delivery attempts, a
30-day receipt retention period, and health port `8080`. Override the retry
count with `NGINX_CONFIG_MANAGER_MAX_ATTEMPTS`, retention with
`NGINX_CONFIG_MANAGER_INBOX_RETENTION` (Go duration syntax), and health port
with `NGINX_CONFIG_MANAGER_HEALTH_PORT`. The inbox directory must remain on
persistent storage across restarts so committed Kafka offsets remain replay-safe.

`NGINX_RELOAD_ENABLED=true` is rejected at startup because this container does
not receive the Docker socket. Reload the gateway through an approved
deployment operation instead; this prevents partial config writes from being
treated as completed automation events.
