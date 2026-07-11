import CopyButton from './CopyButton'

interface Props {
  prompt: string
  badge?: string        // 'AI' | 'AGENT' | ...
  label?: string        // optional explainer next to the badge
  mono?: boolean        // render prompt body in monospace
  maxHeight?: number    // default 180
}

export default function PromptCard({
  prompt,
  badge = 'AI',
  label,
  mono = false,
  maxHeight = 260,
}: Props) {
  const bodyStyle: React.CSSProperties = mono
    ? {
        margin: 0,
        padding: 14,
        fontFamily: 'var(--font-mono)',
        fontSize: 12.5,
        lineHeight: 1.55,
        color: 'var(--text)',
        background: 'var(--bg-soft)',
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
        maxHeight,
        overflow: 'auto',
      }
    : {
        padding: '18px 20px',
        fontSize: 14,
        lineHeight: 1.75,
        color: 'var(--text)',
        whiteSpace: 'pre-wrap',
        overflowWrap: 'anywhere',
        maxHeight,
        overflowY: 'auto',
      }

  return (
    <div
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border-strong)',
        borderRadius: 12,
        overflow: 'hidden',
        boxShadow: 'var(--shadow-card)',
      }}
    >
      <div
        style={{
          height: 38,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 10px 0 16px',
          borderBottom: '0.5px solid var(--border)',
          background: 'var(--bg-card)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              color: 'var(--brand-text)',
              padding: '1px 6px',
              background: 'var(--brand-soft)',
              borderRadius: 3,
              letterSpacing: '0.04em',
            }}
          >
            {badge}
          </span>
          {label && <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>{label}</span>}
        </div>
        <CopyButton text={prompt} />
      </div>
      {mono ? <pre tabIndex={0} style={bodyStyle}>{prompt}</pre> : <div tabIndex={0} style={bodyStyle}>{prompt}</div>}
    </div>
  )
}
