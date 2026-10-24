# Maintained Dynaconf profile for Evidently on Python 3.15

The current Dynaconf 3.3.5 wheel caps Python below 3.15 and bundles older code
outside normal installed-package inventories. Its source identifies Box 4.2.3,
Click 8.0.0.dev, python-dotenv 0.13.0, TOML 0.10.1 and ruamel.yaml 0.16.10,
plus a historical TOML reader/writer bundle. The vendor manifest is inaccurate
for Click/TOML and is not substituted for actual source evidence. The dotenv
rewrite still uses a temporary file and shutil.move, matching the reported
cross-device symlink advisory pattern. A zero installed-package scan does not
prove that these embedded modules are current or unaffected.

This derived profile labels itself `dynaconf 3.3.5+hai.evidently.1`. The builder
verifies the exact official wheel, retains all 40 Dynaconf core module bytes,
removes the 75 bundled vendor files, and supplies eight adapters to separately
installed current Box, Click, python-dotenv, TOML, Tomli, Tomli-W and ruamel.yaml
packages. Original MIT and bundled-library license notices remain; historical
vendor version files are provenance, not the selected runtime inventory.
Every RECORD digest and the deterministic ZIP is rebuilt without vendor imports.

Safe YAML loading/dumping uses current YAML instances and the upstream parser;
unsupported unsafe/legacy loader modes fail explicitly. The TOML reader/writer
uses the real current libraries. This is the bounded Evidently settings/report
profile; optional provider/CLI integrations and complete Dynaconf-library
compatibility are outside qualification. It does not replace report/validation
logic with stubs. Existing HAI privacy bounds and application cases are retained.

The restricted public-only metadata job builds the reviewed
wheel and resolves the separate requirements-port.in on exact Python 3.15.1 /
Debian 13. No application source, credentials or Docker socket enter that SDK.
The generated graph and actual artifacts have been verified and integrated.
Fresh existing SDK/image/health cases and strict whole-image scans must qualify
this source. Plotly parent constraints, embedded libraries, system/compiler
versions and independent live acceptance remain separate gaps. No blanket
current/security claim, main merge, deployment or operator database change.

The branch also preserves the unchanged telemetry opt-out patch from #341;
proposal history is merged without changing main. No new application cases,
local SDK/application/native/scanner execution or operator tool install occurs.

The initial public metadata run at c73f9a8 passes both jobs; exact artifact
ZIP/wheel/lock hashes and source/tested trees verify. The 86-package candidate
has no prereleases or per-version feed advisories, but Plotly 5.24.1 remains
constrained below stable 7.1.0. The canonical input and both lock mirrors now
select the real generated graph; CI and both SDK/image/native profile targets
are Python 3.15.1 on Debian 13. Runtime existing cases run as UID 10001 with
network disabled. The native source manifest/builder and all app/case/Pandas
adapter bytes remain unchanged. Actual fresh image/health/scan qualification
remains required; this candidate is not a complete latest-version claim.
