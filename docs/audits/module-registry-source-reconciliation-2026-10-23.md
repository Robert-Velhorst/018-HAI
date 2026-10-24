# Module registry source reconciliation

## Preserved source behavior

The original registry and its complete test delta were reviewed against the
current registry, extracted URL resolver and tests. Retain all 29 original
module definitions, their identity, route, group, primary-action ownership and
disclosure relationships. Current additions are the Hire.AI and Operational
Brain modules and three Advanced section IDs for workflow outcome criteria,
framework preference history and system delivery detail.

The original `moduleForUrl` implementation is retained in `module-url.ts`.
Its Angular URL serializer, matrix-parameter handling, encoded-segment boundary
and safe fallback behavior are not replaced by a handwritten URL parser. The
registry itself stays independent of that Angular import, and its newer
document-title helper remains. Existing disclosure-resolution functions stay
unchanged.

All 22 original registry scenarios and their assertions remain, with the
additional framework-history expectation and newer static section inventory.
The existing successful browser job at published head
`3056fa0e3d22c34d764eaa4d01ec4a52850ae279` includes this unchanged registry/spec
content. That evidence is not a new rendered browser run of local follow-ups.

## Added low-resource checks

Five native checks now exercise real registry data and functions alongside the
existing entry-point contract. They check unique module IDs and routes,
per-module section resolution, primary-action ownership, route/registry
bijection, nested Basic/Advanced inheritance and malformed ancestor chains.
Negative fixtures reject duplicate identities/routes, unknown groups,
contradictory disclosure modes, missing parents and cycles. These checks run
through the existing shared native command without another component framework
or Docker environment.

The full local native frontend run passes 271 tests with no failures or skips;
the focused registry/entry target passes ten tests as a subset. No production
registry replacement, dependency installation, account change or deployment
was needed. Metadata consistency is not proof that every registered remote
integration or page action has passed live acceptance.

Original sources and transcript archives remain retained. These two file
reviews do not complete the broader checkout/clone/archive scope, establish
main preservation or authorize source deletion.

## Indexed transcript variants

Three historical registry patch variants were also read directly from their
indexed archive offsets, with the frozen index hash, patch/content fingerprints
and source size/mtime checked. The initial twenty module records and three
navigation groups remain represented in the newer registry; the model-health
description is deliberately superseded by the newer outcome-focused wording.
Both initial registry test scenarios retain their assertion bodies within the
current twenty-two scenarios. The later Brain catalog addition also remains.

The initial handwritten query/fragment splitter is not restored over the
current Angular URL serializer. Its ordinary registered-route behavior remains
covered while matrix parameters and encoded segment boundaries retain the
newer handling. No historical patch was executed as an instruction.

This is a semantic disposition of those three indexed code variants only.
Complete archive SHA-256 hashes were not reverified in this bounded pass, and
other patches, surrounding tool results, malformed records and non-code context
remain outside it. The archives are not eligible for deletion from this result.
