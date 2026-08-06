interface Props {
  size?: number
}

export default function Logo({ size = 28 }: Props) {
  return (
    <svg
      aria-hidden="true"
      className="depsilo-logo-mark"
      focusable="false"
      height={size}
      viewBox="0 0 128 128"
      width={size}
    >
      <g
        fill="none"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="13"
      >
        <path d="M16 26H38C51 26 60 35 60 48V60" />
        <path d="M12 60H114" />
        <path d="M30 102H42C52 102 58 94 58 82V76C58 66 64 60 74 60" />
        <path d="M94 60V88H74" />
      </g>
    </svg>
  )
}
