# Trello webhooks

HAI can receive signed Trello board callbacks at:

```text
POST /api/v1/sources/webhooks/trello
HEAD /api/v1/sources/webhooks/trello
```

The endpoint is intentionally outside browser-session authentication. It accepts a callback only when the `X-Trello-Webhook` HMAC-SHA1 signature matches the exact raw request body concatenated with `TRELLO_WEBHOOK_CALLBACK_URL`, using the Trello application secret (`TRELLO_API_SECRET`) as the signing key. This is separate from the API key used by HAI's read-only Trello client. The callback URL must be the exact public HTTPS URL registered with Trello, including any reverse-proxy prefix. Trello uses `HEAD` to check callback reachability.

## Configuration

1. Configure the existing owner-bound Trello read-only settings: `TRELLO_API_KEY`, `TRELLO_READ_TOKEN`, `TRELLO_ACCOUNT_OWNER_IDENTITY`, and `TRELLO_ACCOUNT_MEMBER_ID`.
2. Set `TRELLO_WEBHOOK_CALLBACK_URL` to the public HTTPS URL shown above.
3. Set `TRELLO_API_SECRET` to the application secret for the Trello app that owns the read token. Do not use the API key or token as a substitute.
4. Apply HAI database migrations, including `0103_trello_signed_webhook_intake`.
5. Keep the durable source worker enabled and healthy. HAI returns `503` rather than acknowledging a signed event it cannot durably queue.
6. Register the board webhook with Trello using the exact callback URL. Registration is an operator action; HAI does not create or delete Trello webhooks and does not write to Trello.

## Processing behavior

HAI validates and size-bounds the callback before storing a minimal receipt. It does not retain the raw callback body or signature. `(source, action ID)` is an idempotency key. The durable worker associates the board only with the configured HAI owner and connected Trello source. Unknown boards receive `410 Gone`; an unavailable worker receives `503` so the sender is not told that work was accepted.

Board changes queue the existing durable read-only board reconciliation. `commentCard` and `copyCommentCard` callbacks may add source-linked comment evidence. Edit/delete callbacks follow a stricter path: the supported decoder reads the root `action.id`, `action.data.card.id`, and `action.data.text`, but it does not expose an original comment action ID. HAI therefore does not assume the callback action ID identifies the previously imported comment and does not import callback text as current evidence.

For an edit/delete callback, HAI does not treat the callback action ID as the original comment action ID and does not use callback text as the current comment. The durable worker runs an owner-bound, read-only refresh while holding the source-sync lease. It verifies the configured board, fetches the one affected card and its list, then reads that card's paginated comment actions using `GET /1/cards/{id}/actions?filter=commentCard,copyCommentCard`. Each response must identify the requested card, and all action pages must pass identity, ordering, record, byte, and request limits before reconciliation begins. HAI imports the current comment actions and refreshes that card's snapshot. It never requests comments for another card.

Trello exposes `updateComment` and `deleteComment` as webhook action types, but excludes them from nested action resources; current comment text and active comment identities therefore come from the card's filtered `commentCard` and `copyCommentCard` action list, not from nested `updateComment`/`deleteComment` history. If a previously imported comment action is absent from the fully read current action list, HAI archives only that action's extraction for the affected card and records a provider-deletion audit entry. Its raw source item and extraction text remain available as history. A later complete card refresh can restore an extraction only when it carries HAI's explicit provider-deletion marker and Trello again returns that same action as active. Other archive states are not automatically undone.

The webhook receipt and audit records are retained. The receipt stores the verified callback fields, including its comment text and body fingerprint, but not the original HTTP body or signature. Board cursors and the last full-board-sync timestamp are not advanced by this scoped refresh. If the board/card identity is wrong, any page is incomplete, a returned action belongs to another card, or item persistence fails, the refresh fails closed: the receipt is retried and no missing action is archived. Retries are idempotent because active comments use Trello action IDs and provider-deleted extractions have an explicit state marker.

HAI only uses GET requests for this refresh. Trello's comment edit and delete REST operations are not called. The existing source-sync lease serializes this operation with source sync work; an active durable board scan causes the webhook job to defer rather than race it.

For the provider contracts, see the official [Trello Action Types guide](https://developer.atlassian.com/cloud/trello/guides/rest-api/action-types/), [Cards REST API reference](https://developer.atlassian.com/cloud/trello/rest/api-group-cards/), [nested resources guide](https://developer.atlassian.com/cloud/trello/guides/rest-api/nested-resources/), and [webhooks guide](https://developer.atlassian.com/cloud/trello/guides/rest-api/webhooks/).

For Trello's signature contract and callback lifecycle, see the [webhook API reference](https://developer.atlassian.com/cloud/trello/rest/api-group-webhooks/).
