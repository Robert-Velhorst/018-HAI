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

`NGINX_RELOAD_ENABLED=true` deliberately fails closed because this container
does not receive the Docker socket. Reload the gateway through an approved
deployment operation instead.
