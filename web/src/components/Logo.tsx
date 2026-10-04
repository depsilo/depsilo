interface Props {
  size?: number
}

// Depsilo brand mark: an open repository boundary (two blue shell halves) around
// a cached dependency module (green). Masters live in docs/brand/; keep this
// inline copy in sync with docs/brand/icon-light.svg.
export default function Logo({ size = 28 }: Props) {
  return (
    <svg
      aria-hidden="true"
      className="depsilo-logo-mark"
      focusable="false"
      height={size}
      viewBox="0 0 256 256"
      width={size}
    >
      <path d="M28 76 L108 30 L108 62 L60 90 L60 166 L108 194 L108 226 L28 180 Z" fill="var(--logo-mark)" />
      <path d="M124 20 L228 80 L228 176 L124 236 L124 204 L196 162 L196 98 L124 56 Z" fill="var(--logo-mark-light)" />
      <path d="M128 88 L170 112 L128 136 L86 112 Z" fill="var(--logo-mark-accent)" />
      <path d="M86 122 L128 146 L170 122 L170 160 L128 184 L86 160 Z" fill="var(--logo-mark-accent-deep)" />
      <path d="M86 122 L128 146 L170 122 L128 150 Z" fill="var(--logo-mark-accent)" />
    </svg>
  )
}
