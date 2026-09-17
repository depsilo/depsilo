interface LogoProps {
  size?: number
}

/**
 * Placeholder mark for Stage A.
 *
 * The previous brand mark is deliberately not preserved: Stage B owns the
 * product identity, and a neutral placeholder keeps the shell honest until
 * then. It takes its colour from `currentColor` so it tracks the surface.
 */
export default function Logo({ size = 28 }: LogoProps) {
  return (
    <svg
      data-brand-mark
      aria-hidden="true"
      focusable="false"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className="shrink-0"
    >
      <rect x="3.5" y="3.5" width="17" height="17" rx="4.5" stroke="currentColor" strokeWidth="2" />
      <path
        d="M9 8.5v7M9 8.5h3.4a3.5 3.5 0 0 1 0 7H9"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
