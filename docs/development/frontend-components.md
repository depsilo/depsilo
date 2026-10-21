# Frontend component ownership

Admin, Portal, and Setup share the shadcn `base-nova` component family under
`web/src/components/ui/`. `web/components.json` records the registry style and
aliases. Base UI owns interactive behavior; the checked-in component source
owns project styling and small accessibility adaptations. This is shadcn's
source-distribution model, not a separate versioned component runtime.

## One generic component layer

Import generic controls directly from `@/components/ui/*`. Compose Field,
FieldLabel, a control, and FieldDescription/FieldError at the form boundary.
Use Dialog/Sheet for modal content, Popover for clickable information, Tooltip
for short labels, and Tabs/ToggleGroup/Toggle for the corresponding selection
semantics. For button-style navigation, compose `Button` with
`render={<Link ... />}`, `nativeButton={false}`, and `role="link"`. Retain
ordinary links for inline navigation.

The former Button/Badge/Input/Select/Switch/Modal/Drawer/Tabs/Tooltip wrappers
and V2 aliases are removed. Do not recreate an adapter solely to rename props,
forward children, or restyle a standard component. Fix shared behavior in its
owning `ui` file and keep page layout at the caller.

Standard source includes these deliberate project adaptations:

- Form controls use Base UI Field relationships. Explicit accessible names
  take precedence over generated labels; validation feedback keeps its ID.
- NativeSelect remains available for native form semantics. Rich connection
  selectors use Select; both resolve popup colors through the same theme.
- Tooltip and detailed metric Popover copy use an explicit 14px size so the
  application's compact root font does not shrink help text.
- Dialog and Sheet close controls are localized and have a 40px target.
  Callers retain pending-operation dismissal guards and focus destinations.
- TableContainer is a named, focusable local scroll region. Table itself does
  not add another scroll wrapper. Empty/error states live outside that region.
- Capacity and bounded counts use Meter. Transfer completion uses Progress;
  unknown totals use its indeterminate state. An unknown quota has no meter.
- Alert's success/warning/error colors use existing semantic tokens. Skeleton
  and indeterminate progress respect reduced motion.

## Theme and business compositions

`web/src/index.css` owns the theme. Route-specific layout CSS loads with its
route (including PortalApp, Dashboard, and ConnectProject), keeping it out of
the initial shared assets. Generic components use shadcn semantic
roles. Existing compatibility aliases resolve to those roles; Dashboard
surfaces and status colors also resolve through this shared theme. Chart
series, the blue Dashboard action accent, and Portal branding retain explicit
semantic roles rather than creating another generic component layer.

Keep compositions that own product behavior or a repeated layout contract:
DataTable's typed column projection, QueryErrorState's retry state,
SectionHeader's responsive heading/action layout, Metric's directional delta
meaning, StatusDot's health mapping, and the icon/brand catalogs. Toast adapts
the application's notification interface to the existing Sonner provider.
Dashboard, connection verification, upstream, policy, and request components
remain product modules built from shared primitives.

Activity charts continue to use the installed Recharts implementation. Their
bucket scope, sample counts, units, and request-path series are domain data;
they must not be replaced with demo data or changed to match a visual example.
The current chart uses a shared plotting area with gradient fills. Resource
sparklines remain a compact secondary view.

## Updating components

Review upstream source against the current `base-nova` style; never overwrite
all local files blindly. No runtime package versions changed in this migration.
Preserve the documented adaptations above, inspect the diff, then run focused
browser tests and the shared-primitive gate in [testing.md](testing.md).
That gate includes keyboard/focus, forms, mutation failures, contrast, mobile
scroll containment, and Admin/Portal/Setup flows. Also run the production UI
smoke when the embedded frontend is delivered.
