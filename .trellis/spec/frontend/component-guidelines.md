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
- Form inputs require labels and error text connected with `aria-describedby` where applicable.
- Charts require a textual summary/table alternative for the current value and failure state.
- Do not communicate online/offline/severity by color alone.

## Styling

- Current styling is plain CSS with tokens in `:root`; no CSS framework or CSS-in-JS library is established.
- Keep layout responsive from the 320px minimum shown in `styles.css`.
- Reuse CSS custom properties for stable colors/spacing before duplicating magic values across feature styles.
- Adding a styling framework is a design change, not an incidental component dependency.

## Tests

- Test user-visible semantics and behavior, not internal component structure. `App.test.tsx` renders the component and asserts stable content.
- Interactive components need keyboard, disabled/loading/error, and empty-state cases.
- Avoid snapshots as the only assertion for API/security-sensitive content.

## Avoid

- `default` component exports without a project-wide need.
- Components that cast raw API responses.
- Direct DOM queries outside focused bootstrap/integration code.
- Rendering internal hostnames, IPs, OS/kernel details, or history into public-page components even if a backend bug supplies them.
