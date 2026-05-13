import iconLight from '@/assets/icon-light.svg'
import iconDark from '@/assets/icon-dark.svg'

interface Props {
  size?: number
}

export default function Logo({ size = 28 }: Props) {
  return (
    <>
      <img src={iconDark} width={size} height={size} alt="depsilo" className="logo-icon-light" />
      <img src={iconLight} width={size} height={size} alt="" aria-hidden className="logo-icon-dark" />
    </>
  )
}
