# Evidently source port for Plotly 7

Upstream Evidently 0.7.23 pins Plotly below 6. The current public Plotly release
is 7.1.0. Its official version 6/7 migration guides identify incompatible APIs;
the actual 7.1.0 wheel no longer contains the two figure factories called by
Evidently's classification renderers.

`build_evidently_wheel.py` derives explicitly modified
`evidently 0.7.23+hai.plotly.1` from the SHA-256-bound official wheel. It replaces
the two factory call sites using public Plotly 7 graph objects:

- Probability distributions retain both probability-density histograms,
  0.05-width bins, class colors, overlay, sample rugs and legend groups. The
  original call explicitly disabled the KDE curve; no statistical curve is
  removed by the port. Separate subplot axes preserve the rug labels.
- Classification quality retains numeric heatmap values, original rounded
  text, category/metric labels, color scale and color bar. Public Heatmap text
  rendering replaces the removed annotation factory.

The third source change marks `__version__` as the local port. All other 591
Python modules, including report/data-summary calculations, are byte-identical
to upstream. Original notices, assets and wheel data remain intact. Requirements
replace only the Plotly constraint; optional dependency declarations are retained.
RECORD is regenerated and HAI_PORT.json records upstream and modified identities.
The fixed ZIP metadata avoids platform/time-dependent wheel hashes.

This is a maintained HAI compatibility port, not an upstream Evidently release.
Current source audits and wheel identity do not establish chart pixel parity,
typed-array compatibility with every embedded frontend, interactive browser
privacy, image export, optional provider behavior or complete vendor API parity.
The owned runner does not export a report/HTML/figure or invoke Plotly's UI/cloud
sharing paths. Its existing synthetic/redacted fixture bounds and response remain
unchanged. No new application cases are introduced.

The restricted public-only metadata job retains both port wheels, generated graph
and build/resolver provenance. Runtime integration follows verification of that
graph. Keep the draft unmerged until supported existing cases and the remaining
image/native/privacy gates are reconciled. No local vendor execution is needed
for wheel construction.

Primary sources:

- https://pypi.org/pypi/evidently/0.7.23/json
- https://pypi.org/pypi/plotly/7.1.0/json
- https://plotly.com/python/v6-migration/
- https://plotly.com/python/v7-migration/

## Public source-digest scanner provenance

The full upstream module manifest includes filenames containing `auth`, `token`
and `api`. Gitleaks classified five adjacent SHA-256 values as generic API keys.
Each value was independently recomputed from the hash-verified public Evidently
wheel. They are source-integrity digests and authenticate no service. Five exact
historical commit/path/rule/line fingerprints in `.gitleaksignore` record only
these reviewed false positives. No file path, detector, image advisory, future
value or other finding is excluded; the redacted scanner remains unchanged.
