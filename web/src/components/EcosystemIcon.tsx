import {
  siPython,
  siUbuntu,
  siNpm,
  siGo,
  siRust,
  siApachemaven,
  siRuby,
  siPhp,
  siDotnet,
  siAnaconda,
  siR,
  siHelm,
} from 'simple-icons'

type EcosystemType =
  | 'pip' | 'pypi'
  | 'apt'
  | 'npm'
  | 'go' | 'goproxy'
  | 'cargo' | 'crates'
  | 'maven'
  | 'rubygems'
  | 'composer'
  | 'nuget'
  | 'conda'
  | 'cran'
  | 'helm'

interface EcosystemIconProps {
  type: EcosystemType
  size?: number
  useColor?: boolean
  className?: string
}

const iconMap: Record<string, typeof siPython> = {
  pip: siPython,
  pypi: siPython,
  apt: siUbuntu,
  npm: siNpm,
  go: siGo,
  goproxy: siGo,
  cargo: siRust,
  crates: siRust,
  maven: siApachemaven,
  rubygems: siRuby,
  composer: siPhp,
  nuget: siDotnet,
  conda: siAnaconda,
  cran: siR,
  helm: siHelm,
}

// Icons whose brand color is too dark for dark mode
const darkColorIcons = new Set(['cargo', 'crates', 'go', 'goproxy'])

export default function EcosystemIcon({
  type,
  size = 16,
  useColor = true,
  className = '',
}: EcosystemIconProps) {
  const icon = iconMap[type]
  if (!icon) return null

  const brandColor = `#${icon.hex}`
  const isDarkMode =
    typeof document !== 'undefined' &&
    (document.documentElement.getAttribute('data-theme') === 'dark' ||
      document.documentElement.classList.contains('dark'))

  const shouldFallback = isDarkMode && darkColorIcons.has(type)
  const color = useColor && !shouldFallback ? brandColor : 'currentColor'

  return (
    <svg
      role="img"
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill={color}
      className={className}
      aria-label={icon.title}
    >
      <path d={icon.path} />
    </svg>
  )
}
