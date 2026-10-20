# Hire.AI Connector

HAI includes an optional outbound client for Hire.AI's authenticated, read-only remote-job search endpoint. It is disabled by default. When enabled, authenticated HAI owners with read permission can query current, canonical, safety-cleared remote listings through HAI. HAI does not forward candidate profiles, resumes, credentials, or application records.

## Configuration

Use a dedicated bearer token generated for this connector. Store the same token in the Hire.AI and HAI secret stores; do not put it in chat, source control, issue reports, or logs.

In Hire.AI, configure its inbound connector for the intended candidate account:

```dotenv
HAI_CONNECTOR_ENABLED=true
HAI_CONNECTOR_TOKEN=<dedicated-random-token-at-least-32-characters>
HAI_CONNECTOR_USER_ID=<Hire.AI-user-id>
HAI_CONNECTOR_URL=https://013-hire.ai.noodzakelijkonline.de/api/hai/a2a
HAI_CONNECTOR_ALLOWED_ORIGINS=https://013-hire.ai.noodzakelijkonline.de
```

In HAI, set:

```dotenv
HIRE_AI_CONNECTOR_ENABLED=true
HIRE_AI_JOBS_SEARCH_URL=https://013-hire.ai.noodzakelijkonline.de/api/hai/jobs/search
HIRE_AI_CONNECTOR_TOKEN=<same-dedicated-random-token>
HIRE_AI_CONNECTOR_TIMEOUT=8s
```

Production requires a public HTTPS hostname without an explicit port. HAI resolves the configured hostname for each new connection and dials only an accepted address directly. It refuses loopback, private, shared-CGNAT, link-local, multicast, reserved, benchmarking, documentation, and other non-global special-purpose destinations; IPv6 must be in global-unicast space and must not use a non-global special-purpose allocation. Only explicit `RUN_MODE=demo` or `RUN_MODE=test` enables local development endpoints such as loopback or `host.docker.internal`. Missing or unrecognized `RUN_MODE` values default to production restrictions. Redirects and environment proxy settings are not followed.

## HAI API

Both routes require the normal authenticated HAI owner session and read permission:

- `GET /api/v1/hire-ai/status` reports enabled/configured state and restrictions without returning the bearer token.
- `POST /api/v1/hire-ai/jobs/search` forwards a bounded search request to the exact configured Hire.AI endpoint.

Authenticated users can open **Hire.AI jobs** in HAI's work navigation. The page shows connector readiness, offers the supported keyword, location, employment, source-platform, salary, experience, application-system (Greenhouse, Lever, Ashby, SmartRecruiters, Workday, email, or other), posting-age, currency, and listing-preference filters, and supports cursor pagination. Salary bounds require a complete ordered range and a three-letter currency. Each returned page is checked again for the remote-only, safety-cleared, canonical-listing policy before display. Job links open the source or employer page in a separate tab; HAI does not automate the application.

Example request:

```json
{
  "filters": {
    "query": "customer support",
    "location": "Europe",
    "jobType": "full-time",
    "experienceLevel": "all"
  },
  "limit": 10
}
```

The request body is limited to 16 KiB; the result to 512 KiB; page size to 25; and the outbound call to the configured timeout (maximum 30 seconds). Use the returned `nextCursor` for subsequent pages. Hire.AI enforces the canonical, remote-only, safety-cleared policy. HAI verifies that policy in the response and rejects malformed, oversized, non-public HTTPS listing URLs, redirects, unsuccessful upstream responses, and a next cursor that exactly repeats the current cursor. The HAI page also stops if a cursor repeats anywhere in the current search sequence, preventing loops and duplicate page appends. Failures return a generic error without exposing upstream response bodies or credentials.

This connector does not submit applications, send email, change approvals, share candidate information, or start workflows. The Hire.AI A2A endpoint remains a separate inbound capability; configuring this job-search connector does not enable arbitrary A2A calls.

## Verification boundary

The HAI unit tests use a local HTTP test server and verify request authentication, filter bounds, response policy, redirect handling, destination restrictions, and repeated-cursor rejection. On 2026-10-19, `go test ./internal/hireai ./internal/router` passed after the cursor-progress guard was added, using the `golang:1.27.2` Docker image. The focused `HireAIComponent` Angular suite previously passed in an isolated Node/Chromium container: 9/9 tests, including the repeated-cursor regression. A native Windows Chrome Headless run on 2026-10-19 passed the focused Angular suite after adding overlap deduplication: 11/11 tests, including preserving first-seen order and refreshing duplicate listing data by canonical job ID. Earlier on 2026-10-19, `go vet ./internal/hireai ./internal/router`, `npm run check:control-room`, the production build, and all 1,393 Angular tests passed; those broader checks predate the latest cursor and deduplication changes and are not claimed as post-change verification. The production build completed with existing initial-bundle and SCSS budget warnings. `bash ../scripts/audit-frontend-production-dependencies.sh` passed. The full frontend audit reports six high findings, all from the same development/test-only dependency path: `braces` through Karma's `chokidar` watcher, propagated to `karma`, `karma-jasmine`, `karma-jasmine-html-reporter`, and `@angular/build`. GitHub Advisory `GHSA-vfj7-8cjw-p6xm` currently lists no patched `braces` release; npm's suggested fixes require major-version downgrades of the Karma toolchain. No downgrade or audit suppression was applied. Recheck this advisory before release and update the test-toolchain dependency path when a compatible upstream fix exists. These checks do not prove a live Hire.AI instance, real credentials, DNS/TLS, or candidate-account configuration. Complete live acceptance only after both services are configured and an authenticated search returns expected listings.

On 2026-10-19, the latest focused check after making the connector share HAI's fail-closed run-mode parser passed `go test ./internal/hireai ./internal/router` and `go vet ./internal/hireai ./internal/router` in `golang:1.27.2`. Regression tests cover missing and unknown run modes as production-strict and explicit demo mode for local endpoints. This does not change the live-acceptance boundary above.

On 2026-10-20, the browser request timeout for `POST /api/v1/hire-ai/jobs/search` was aligned to the connector's 30-second maximum plus a bounded five-second response margin; unrelated operations retain their existing 30-second limit. The focused Hire.AI page and HTTP-timeout specs passed 22/22 in Chrome; the full frontend workflow then passed all Node contract tests and 1,399/1,399 Angular tests on Node.js 24.19.0. All five control-room compiler checks passed, the production build completed, and the full Go backend suite (`go test -p 1 ./...`) plus `go vet ./...` passed using Go 1.27.2. The package is explicitly ESM to avoid Node's module-reparse warnings while its Node configuration scripts remain `.cjs`. Existing initial JavaScript and four stylesheet budget warnings remain; bundle statistics show the 397.6 KB global theme as the largest initial payload input, so safely reducing it requires route-level style loading rather than deleting styles used by lazy modules. This verifies local build and test behavior, not a live cross-project connector request.

Later on 2026-10-20, NG-ZORRO styles were split into an injected login theme and a separate authenticated theme loaded by the authenticated child-route guard. The production build emits the latter as a non-injected asset, and the build wrapper writes a SHA-256 cache-busting manifest; the service only accepts same-origin stylesheet URLs and keeps navigation available if theme loading fails. The production initial bundle decreased from 1.01 MB to 792.48 kB raw (estimated transfer 181.35 kB); 309.25 kB of component styles are deferred until an authenticated route. The full frontend test workflow passed 180 Node contracts and 1,401/1,401 Angular tests, including the new manifest and stylesheet-loader cases. Chrome viewport emulation at 390x844 confirmed no horizontal overflow (390 px document/root; 366 px login panel), and the authenticated stylesheet returned HTTP 200 from the local development server. Four existing component SCSS budget warnings remain. The local backend was not running during the login render check, so auth-service error states were expected; no live HAI/Hire.AI integration, deployment, DNS, or HTTPS acceptance was performed.

Focused connector recheck on 2026-10-20: with the current local HAI branch, `go test ./internal/hireai ./internal/router` passed in a disposable `golang:1.27.2` container, and `HireAIComponent` passed 13/13 ChromeHeadless tests on Node.js v24.22.0. This checkout contains uncommitted HAI changes on `codex/hire-ai-connector`; they have not been merged into HAI `main`. The tests use local boundaries only and do not prove public HTTPS, real shared credentials, or an authenticated request between deployed HAI and Hire.AI.


Dependency advisory recheck on 2026-10-20: `npm audit --omit=dev --json` returned zero production vulnerabilities for the frontend lockfile. The full audit returned six high findings on the Karma/Chokidar development-test dependency path, including `braces@3.0.3`; GitHub Advisory `GHSA-vfj7-8cjw-p6xm` still lists no patched release. The frontend Dockerfile uses a multi-stage build and copies only `dist/app` into the final Nginx image, excluding the build-stage `node_modules`; the finding therefore remains a build/test-toolchain risk, not a frontend runtime-image dependency. The repository's Bash audit wrapper could not run in this Windows/WSL invocation because WSL did not have `node` on its PATH; the equivalent npm audits were run directly with Windows Node/npm. No package downgrade or audit suppression was made. This source inspection does not substitute for inspecting a built image or the full production server deployment.
