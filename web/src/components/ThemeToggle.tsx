export default function ThemeToggle() {
  return (
    <button
      disabled
      aria-disabled="true"
      aria-label="Light mode (fixed)"
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 28,
        height: 28,
        background: 'transparent',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        color: 'var(--text-muted)',
        cursor: 'default',
        padding: 0,
      }}
    >
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.2"/>
        <g stroke="currentColor" strokeWidth="1.2" strokeLinecap="round">
          <path d="M8 1.5v1.5M8 13v1.5M14.5 8H13M3 8H1.5M12.6 3.4l-1 1M4.4 11.6l-1 1M12.6 12.6l-1-1M4.4 4.4l-1-1"/>
        </g>
      </svg>
    </button>
  )
}
