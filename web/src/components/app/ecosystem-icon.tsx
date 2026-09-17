import alpineSvg from 'simple-icons/icons/alpinelinux.svg?raw'
import anacondaSvg from 'simple-icons/icons/anaconda.svg?raw'
import dockerSvg from 'simple-icons/icons/docker.svg?raw'
import dotnetSvg from 'simple-icons/icons/dotnet.svg?raw'
import goSvg from 'simple-icons/icons/go.svg?raw'
import helmSvg from 'simple-icons/icons/helm.svg?raw'
import huggingFaceSvg from 'simple-icons/icons/huggingface.svg?raw'
import mavenSvg from 'simple-icons/icons/apachemaven.svg?raw'
import npmSvg from 'simple-icons/icons/npm.svg?raw'
import phpSvg from 'simple-icons/icons/php.svg?raw'
import pythonSvg from 'simple-icons/icons/python.svg?raw'
import rSvg from 'simple-icons/icons/r.svg?raw'
import rubySvg from 'simple-icons/icons/ruby.svg?raw'
import rustSvg from 'simple-icons/icons/rust.svg?raw'
import ubuntuSvg from 'simple-icons/icons/ubuntu.svg?raw'
import type { EcosystemType } from '@/lib/ecosystemTypes'
import { useResolvedTheme } from '@/lib/theme'

export type { EcosystemType } from '@/lib/ecosystemTypes'

interface EcosystemIconProps {
  type: EcosystemType
  size?: number
  useColor?: boolean
  className?: string
  /** Hide the icon from assistive technology when adjacent text already names it. */
  decorative?: boolean
  /** Override the upstream icon title when the icon carries meaning on its own. */
  label?: string
}

interface IconData {
  title: string
  hex: string
  path: string
}

function parseIcon(svg: string, hex: string): IconData {
  const title = /<title>([^<]+)<\/title>/.exec(svg)?.[1]
  const path = /<path d="([^"]+)"\s*\/>/.exec(svg)?.[1]
  if (!title || !path) throw new Error('Invalid Simple Icons SVG asset')
  return { title, hex, path }
}

const pythonIcon = parseIcon(pythonSvg, '3776AB')
const ubuntuIcon = parseIcon(ubuntuSvg, 'E95420')
const npmIcon = parseIcon(npmSvg, 'CB3837')
const goIcon = parseIcon(goSvg, '00ADD8')
const rustIcon = parseIcon(rustSvg, '000000')
const mavenIcon = parseIcon(mavenSvg, 'C71A36')
const rubyIcon = parseIcon(rubySvg, 'CC342D')
const phpIcon = parseIcon(phpSvg, '777BB4')
const dotnetIcon = parseIcon(dotnetSvg, '512BD4')
const anacondaIcon = parseIcon(anacondaSvg, '44A833')
const rIcon = parseIcon(rSvg, '276DC3')
const helmIcon = parseIcon(helmSvg, '0F1689')
const dockerIcon = parseIcon(dockerSvg, '2496ED')
const huggingFaceIcon = parseIcon(huggingFaceSvg, 'FFD21E')
const alpineIcon = parseIcon(alpineSvg, '0D597F')

const iconMap: Record<EcosystemType, IconData> = {
  pip: pythonIcon,
  pypi: pythonIcon,
  apt: ubuntuIcon,
  npm: npmIcon,
  go: goIcon,
  goproxy: goIcon,
  cargo: rustIcon,
  crates: rustIcon,
  maven: mavenIcon,
  rubygems: rubyIcon,
  composer: phpIcon,
  nuget: dotnetIcon,
  conda: anacondaIcon,
  cran: rIcon,
  helm: helmIcon,
  docker: dockerIcon,
  huggingface: huggingFaceIcon,
  alpine: alpineIcon,
}

// Icons whose brand color is too dark for dark mode
const darkColorIcons = new Set(['cargo', 'crates', 'go', 'goproxy'])

export default function EcosystemIcon({
  type,
  size = 16,
  useColor = true,
  className = '',
  decorative = false,
  label,
}: EcosystemIconProps) {
  const resolvedTheme = useResolvedTheme()
  const icon = iconMap[type]
  if (!icon) return null

  const brandColor = `#${icon.hex}`
  const shouldFallback = resolvedTheme === 'dark' && darkColorIcons.has(type)
  const color = useColor && !shouldFallback ? brandColor : 'currentColor'

  return (
    <svg
      role={decorative ? undefined : 'img'}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill={color}
      className={className}
      focusable="false"
      aria-hidden={decorative || undefined}
      aria-label={decorative ? undefined : (label ?? icon.title)}
    >
      <path d={icon.path} />
    </svg>
  )
}
