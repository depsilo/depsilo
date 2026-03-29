interface IconProps {
  name: string
  className?: string
  size?: 'sm' | 'md' | 'lg'
}

export default function Icon({ name, className = '', size = 'md' }: IconProps) {
  const sizeClass = size === 'sm' ? 'icon-sm' : size === 'lg' ? 'icon-lg' : ''
  return <span className={`icon ${sizeClass} ${className}`}>{name}</span>
}
