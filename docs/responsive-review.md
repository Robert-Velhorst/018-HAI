# Responsive & Browser Compatibility Review

Records the responsive approach and the concrete measures applied to the new
components.

## Approach

- **Fluid containers:** new pages use `max-width` + centered layout with
  percentage widths, so they adapt from mobile to desktop without fixed pixel
  widths.
- **Breakpoint:** a `@media (max-width: 480px)` rule tightens padding and makes
  action buttons full-width / stacked on small screens (onboarding, exceptions,
  quick-capture).
- **Flexible action bars:** button rows use flexbox with `flex-wrap` and a spacer
  that collapses on narrow screens.
- **Tables:** the exceptions table is small-sized and lives in a card that scrolls
  within its container rather than forcing horizontal page scroll.

## Browser compatibility

- Built with Angular 20 + ng-zorro 20, targeting evergreen browsers (Chrome, Edge,
  Firefox, Safari) per the project's `browserslist`.
- No use of non-standard CSS; layout relies on flexbox/media queries supported
  across evergreen browsers.
- Prior fix `7ca5294` addressed mobile navigation; new pages follow the same
  mobile-first padding pattern.

## Checklist

| Viewport | New components |
| --- | --- |
| Mobile (≤480px) | Padding reduced, buttons stack/full-width, no horizontal scroll |
| Tablet/desktop | Centered, max-width cards |
| Landscape/rotation | Fluid — no fixed heights that clip content |

## Gaps / follow-ups

1. Add mid-range breakpoints (e.g. 768px) if denser layouts are desired on
   tablets.
2. Run a cross-browser visual pass (BrowserStack/Playwright screenshots) against
   the running app — pending the compose smoke automation.

## Verdict

New components are responsive by construction (fluid + a small-screen breakpoint,
no horizontal overflow). A cross-browser visual regression pass on the full app
is the remaining step, tracked in the roadmap.
