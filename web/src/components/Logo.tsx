import { useId } from 'react';

interface Props {
  size?: number;
}

export default function Logo({ size = 28 }: Props) {
  const uid = useId().replace(/:/g, '');
  const gradId = `logo-grad-${uid}`;
  const dim = size;

  return (
    <svg
      width={dim}
      height={dim}
      viewBox="0 0 28 28"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%"  stopColor="oklch(0.62 0.18 305)" />
          <stop offset="40%" stopColor="oklch(0.66 0.15 260)" />
          <stop offset="75%" stopColor="oklch(0.72 0.12 210)" />
          <stop offset="100%" stopColor="oklch(0.78 0.13 75)" />
        </linearGradient>
      </defs>
      {/* Rounded square background */}
      <rect width="28" height="28" rx="7" fill={`url(#${gradId})`} />
      {/* Three concentric white circles */}
      <circle cx="14" cy="14" r="9"  fill="none" stroke="white" strokeWidth="2.2" strokeOpacity="0.9" />
      <circle cx="14" cy="14" r="5.5" fill="none" stroke="white" strokeWidth="1.8" strokeOpacity="0.7" />
      <circle cx="14" cy="14" r="2"  fill="white" fillOpacity="0.95" />
    </svg>
  );
}
