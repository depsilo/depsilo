interface IconProps {
  name: string
  className?: string
  size?: 'sm' | 'md' | 'lg'
  style?: React.CSSProperties
}

export default function Icon({ name, className = '', size = 'md', style }: IconProps) {
  const sizeClass = size === 'sm' ? 'icon-sm' : size === 'lg' ? 'icon-lg' : ''
  return <span className={`icon ${sizeClass} ${className}`} style={style}>{name}</span>
}
