# Non-Technical User Simulation

A walk-through of the product from the perspective of a non-technical user, to
catch friction that engineers stop noticing. Each step lists the expected
experience and the honest current state.

## Persona

"Sofie" — a solo professional, comfortable with everyday apps, not with servers,
terminals, or JSON.

## Journey

| Step | Sofie expects | Current state |
| --- | --- | --- |
| 1. Open the app | A clear starting screen | Dashboard loads; **no first-run wizard yet** (gap — phase 105). |
| 2. Understand what it does | Plain-language intro | User guide exists; in-app onboarding is the gap. |
| 3. Add her first note | Simple form, sensible defaults | Templates prefill kind/tags/confidence; works. |
| 4. Find something later | Search box that filters | Memory search (filter/sort/paginate) works via API; **needs UI wiring**. |
| 5. Approve an action | Clear "Approve/Reject" with context | Approval gates present; labels available in EN/NL. |
| 6. Trust the output | Know how reliable an answer is | Quality/confidence band available; grounded answers link evidence. |
| 7. Feel safe | Obvious "stop" control | Emergency stop blocks new execution and visibly reports whether an in-flight runtime supports cancellation; it is visible in the HAI OS overview. |
| 8. Know it's working | A simple "all good" signal | `/readyz` gives ready/not-ready; needs a friendly UI surface. |

## Friction found (feeds roadmap)

1. **No first-run onboarding** — the single biggest non-technical barrier (105).
2. **Search & feature-flag/i18n capabilities are backend-only** — real value that
   Sofie can't yet see until the Angular UI consumes them (TD-7).
3. **Readiness is an endpoint, not a friendly banner** — expose it in the UI.

## Verdict

The engine supports a genuinely non-technical journey, but the last mile is UI:
onboarding and surfacing existing backend capabilities. No step requires Sofie to
touch a terminal for normal use.
