interface LogoProps {
  size?: number
}

/**
 * The Depsilo mark: an open repository boundary around a cached dependency
 * module.
 *
 * This is the flat variant from `docs/brand/mark/depsilo-mark-flat.svg`, which
 * the brand kit nominates for UI and code-driven contexts; the gradient
 * version is for print and large placements. The two blue shells are
 * asymmetric and stay *open* — closing them into a hexagon erases the mark's
 * silhouette — with the green module sitting inside the opening.
 *
 * The mark carries its own colours rather than `currentColor`: the kit fixes
 * them, they are legible on both canvases, and a mark that takes the
 * surrounding text colour is a status icon wearing the brand's shape.
 *
 * Minimum size is 16px, 24px preferred; keep 0.25× the mark's width clear
 * around it.
 */
export default function Logo({ size = 28 }: LogoProps) {
  return (
    <svg
      data-brand-mark
      aria-hidden="true"
      focusable="false"
      width={size}
      height={size}
      viewBox="0 0 256 256"
      className="shrink-0"
    >
      <path d="M28 76 L108 30 L108 62 L60 90 L60 166 L108 194 L108 226 L28 180 Z" fill="#2563EB" />
      <path d="M124 20 L228 80 L228 176 L124 236 L124 204 L196 162 L196 98 L124 56 Z" fill="#3B82F6" />
      <path d="M128 88 L170 112 L128 136 L86 112 Z" fill="#22C55E" />
      <path d="M86 122 L128 146 L170 122 L170 160 L128 184 L86 160 Z" fill="#16A34A" />
    </svg>
  )
}
