# x-web3 UI system

## Product direction

- Product: Web3 education marketplace and role-based operations console.
- Tone: trustworthy, focused, technical, premium dark interface.
- Primary journey: discover course → inspect curriculum → enroll onchain → learn → complete → receive credential.
- Page density: spacious marketing entry, standard product pages, compact admin data views.

## Visual foundation

- Typography: Inter for interface and display; JetBrains Mono for chain data, status, and identifiers.
- Background: `#0A0A0F`; panel `#13131A`; raised surface `#1A1A23`.
- Primary accent: violet `#8B5CF6`; data/link accent: cyan `#38BDF8`.
- Semantic colors: mint `#34D399`, amber `#F59E0B`, rose `#F43F5E`.
- Geometry: 6/12/16px radii with 1px low-contrast borders.
- Motion: 150–220ms for interaction feedback; respect `prefers-reduced-motion`.

## Interaction rules

- Every product domain has a deep-linkable route; do not mount unrelated domains on one page.
- Interactive targets are at least 44px high and have visible keyboard focus.
- Async modules always expose loading, empty, error, retry, and success feedback where applicable.
- Never rely on color alone for status; pair it with text.
- Navigation state is expressed with `aria-current` through routed links.
- Mobile primary navigation collapses into an accessible toggle; account/admin subnavigation remains horizontally scrollable.

## Layout rules

- Main content max width: 1180px.
- Course detail max width: 920px; swap workspace max width: 760px.
- At 900px, learning and admin sidebars collapse to a single-column layout.
- At 760px, course grids become one column and header actions stack.

## Avoid

- Anchor-based pseudo-routing for product domains.
- Modal-only course detail views that cannot be refreshed or shared.
- Decorative neon overload, emoji controls, hidden focus rings, or hover-only actions.
- Raw API calls inside components; use the shared API client and domain adapters.
