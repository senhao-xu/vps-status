# Component Guidelines

## Current component pattern

- Use named function components and named exports, as in `web/src/App.tsx`.
- Keep the DOM bootstrap in `main.tsx`; feature components must not call `createRoot` or access the global mount point.
- Import styles explicitly from the owner that needs them. The current app imports one global stylesheet from `App.tsx`.
- Components receive typed props and render from props/state; they do not fetch through ad hoc module-level side effects.

## Composition

- Start with a local component. Extract only when reused or when one component owns multiple distinct interaction responsibilities.
- Prefer explicit children/props over configuration objects with untyped arbitrary rendering callbacks.
- Route-level components assemble features; reusable presentation components must not depend on authenticated API clients or storage shapes.
- Status/alert state should be represented with explicit unions, not independent booleans that can contradict each other.

## Accessibility

- Use semantic elements and accessible names. The foundation screen demonstrates `<main>`, `<section>`, and `aria-labelledby` in `App.tsx`.
- Every interactive control must work by keyboard and expose a visible focus state.
- Keep text-input, select and textarea focus outlines within the control border (`outline-offset: -3px` for the shared 3px outline), so nearby labels and summaries remain readable. Preserve visible outer focus indicators on checkbox/radio controls, buttons and links.
- Form inputs require labels and error text connected with `aria-describedby` where applicable.
- Charts require a textual summary/table alternative for the current value and failure state.
- Do not communicate online/offline/severity by color alone.

## Styling

- Current styling is plain CSS with tokens in `:root`; no CSS framework or CSS-in-JS library is established.
- Keep layout responsive from the 320px minimum shown in `styles.css`.
- Reuse CSS custom properties for stable colors/spacing before duplicating magic values across feature styles.
- Settings navigation actions need a visible resting boundary and a distinct hover/keyboard-focus treatment; a barely tinted background alone is insufficient. Use theme tokens with readable contrasting text and keep border widths/padding stable between states.
- Adding a styling framework is a design change, not an incidental component dependency.

### Localization and themes

- Put reusable static UI copy in the strictly typed dictionaries in
  `src/i18n.tsx`; render it through `useI18n().t`. Dynamic server error text and
  operator-provided names/descriptions remain unchanged.
- Keep the English dictionary as the translation-key owner and type every
  other dictionary as `Record<TranslationKey, string>`, so missing or extra
  entries fail type checking. Use named placeholders such as
  `t('onlineCount', { online, total })` for dynamic copy.
- Route all theme-dependent colors through CSS custom properties. Classic dark tokens
  live in `:root`; light overrides live in `:root[data-theme="light"]`.
  Emerald overrides use the independent `data-visual-theme="emerald"` selector
  together with the light/dark attribute. Themes share page components and
  never change their data boundaries or remount an installation workflow.
  Components must not branch on the active theme to choose presentation
  colors.
- Use `PreferenceControls` anywhere a route can render independently
  (authentication, administrator, and public screens). Language/mode controls
  are native labeled buttons; the visual-theme selector is a labeled select
  with Emerald, Classic and Follow site options.

## Tests

- Test user-visible semantics and behavior, not internal component structure. `App.test.tsx` renders the component and asserts stable content.
- Interactive components need keyboard, disabled/loading/error, and empty-state cases.
- Avoid snapshots as the only assertion for API/security-sensitive content.

## Avoid

- `default` component exports without a project-wide need.
- Components that cast raw API responses.
- Direct DOM queries outside focused bootstrap/integration code.
- Rendering internal hostnames, IPs, OS/kernel details, or history into public-page components even if a backend bug supplies them.
