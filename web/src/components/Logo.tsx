import iconDark from '@/assets/icon-dark.svg'

interface LogoProps {
  className?: string
  height?: number
}

export default function Logo({ className = '', height = 28 }: LogoProps) {
  return (
    <img
      src={iconDark}
      alt="Depsilo"
      height={height}
      className={className}
      style={{ height }}
    />
  )
}
