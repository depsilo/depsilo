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
  maxHeight = 180,
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
        padding: '12px 14px',
        fontSize: 12,
        lineHeight: 1.6,
        color: 'var(--text)',
        whiteSpace: 'pre-wrap',
        maxHeight,
        overflowY: 'auto',
      }

  return (
    <div
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          height: 32,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 8px 0 12px',
          borderBottom: '0.5px solid var(--border)',
          background: 'var(--bg-card)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--brand)',
              padding: '1px 6px',
              background: 'var(--brand-soft)',
              borderRadius: 3,
              letterSpacing: '0.04em',
            }}
          >
            {badge}
          </span>
          {label && <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{label}</span>}
        </div>
        <CopyButton text={prompt} />
      </div>
      {mono ? <pre style={bodyStyle}>{prompt}</pre> : <div style={bodyStyle}>{prompt}</div>}
    </div>
  )
}
