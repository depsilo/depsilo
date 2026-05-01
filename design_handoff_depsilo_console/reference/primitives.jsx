/* ============================================================
   Depsilo — primitive components
   Sparkline, StatusDot, Tag, Copy button, Tab, CodeBlock
   ============================================================ */

const { useState, useEffect, useRef, useMemo, useCallback } = React;

// ────────────────────────────────────────────────────────────
// Sparkline — real line + faint area fill
// ────────────────────────────────────────────────────────────
function Sparkline({ values, width = 120, height = 24, tone = 'neutral', invert = false, showDot = true }) {
  if (!values || values.length === 0) return null;

  const n = values.length;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = (max - min) || 1;

  const padY = 2;
  const usableH = height - padY * 2;

  const points = values.map((v, i) => {
    const x = (i / (n - 1)) * (width - 1) + 0.5;
    const y = padY + (1 - (v - min) / range) * usableH;
    return [x, y];
  });

  const linePath = points.map((p, i) => (i === 0 ? `M${p[0]},${p[1]}` : `L${p[0]},${p[1]}`)).join(' ');
  const areaPath = `${linePath} L${width - 0.5},${height} L0.5,${height} Z`;

  const gradId = useMemo(() => `spark-${Math.random().toString(36).slice(2, 8)}`, []);
  const areaGradId = `${gradId}-area`;

  // tone-driven gradient stops (subtle two-color shift along x)
  const stops = ({
    ok:      ['var(--spec-3)', 'var(--ok)'],
    warn:    ['var(--warn)', 'var(--spec-4)'],
    danger:  ['var(--danger)', 'var(--spec-1)'],
    brand:   ['var(--spec-1)', 'var(--spec-2)'],
    neutral: ['oklch(0.78 0.04 260)', 'oklch(0.70 0.05 240)'],
  })[tone] || ['var(--text-subtle)', 'var(--text-muted)'];

  const fillStops = ({
    ok:      ['oklch(0.72 0.12 210 / 0.18)', 'oklch(0.58 0.12 155 / 0)'],
    warn:    ['oklch(0.70 0.14 65 / 0.18)', 'oklch(0.78 0.13 75 / 0)'],
    danger:  ['oklch(0.60 0.17 25 / 0.18)', 'oklch(0.62 0.18 305 / 0)'],
    brand:   ['oklch(0.62 0.18 305 / 0.20)', 'oklch(0.66 0.15 260 / 0)'],
    neutral: ['oklch(0.7 0.04 260 / 0.10)', 'oklch(0.7 0.04 260 / 0)'],
  })[tone] || ['rgba(0,0,0,0.05)', 'rgba(0,0,0,0)'];

  const last = points[points.length - 1];
  const dotColor = stops[1];

  return (
    <svg width={width} height={height} style={{ display: 'block', overflow: 'visible' }}>
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%" stopColor={stops[0]} />
          <stop offset="100%" stopColor={stops[1]} />
        </linearGradient>
        <linearGradient id={areaGradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={fillStops[0]} />
          <stop offset="100%" stopColor={fillStops[1]} />
        </linearGradient>
      </defs>
      <path d={areaPath} fill={`url(#${areaGradId})`} />
      <path d={linePath} fill="none" stroke={`url(#${gradId})`} strokeWidth="1.1" strokeLinejoin="round" strokeLinecap="round" />
      {showDot && (
        <circle cx={last[0]} cy={last[1]} r="1.75" fill={dotColor} />
      )}
    </svg>
  );
}

// ────────────────────────────────────────────────────────────
// StatusDot — 6px solid + optional label
// ────────────────────────────────────────────────────────────
function StatusDot({ status, size = 6, live = false }) {
  const color = ({
    healthy: 'var(--ok)',
    degraded: 'var(--warn)',
    failed: 'var(--danger)',
  })[status] || 'var(--text-subtle)';

  return (
    <span
      className={live ? 'dot-live' : ''}
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: '50%',
        background: color,
        color, // for box-shadow currentColor in keyframes
        flexShrink: 0,
      }}
    />
  );
}

// ────────────────────────────────────────────────────────────
// Tag — small pill, tone-aware
// ────────────────────────────────────────────────────────────
function Tag({ children, tone = 'neutral', solid = false, style }) {
  const tones = {
    neutral: { fg: 'var(--text-muted)', bg: 'var(--bg-soft)', bd: 'var(--border)' },
    brand:   { fg: 'var(--brand)',      bg: 'var(--brand-soft)', bd: 'var(--brand-border)' },
    ok:      { fg: 'var(--ok-text)',    bg: 'var(--ok-fill)',    bd: 'var(--ok-border)' },
    warn:    { fg: 'var(--warn-text)',  bg: 'var(--warn-fill)',  bd: 'var(--warn-border)' },
    danger:  { fg: 'var(--danger-text)',bg: 'var(--danger-fill)',bd: 'var(--danger-border)' },
  };
  const t = tones[tone] || tones.neutral;
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '2px 8px',
        fontSize: 11,
        fontWeight: 500,
        color: solid ? '#fff' : t.fg,
        background: solid ? t.fg : t.bg,
        border: `0.5px solid ${solid ? 'transparent' : t.bd}`,
        borderRadius: 'var(--r-tag)',
        lineHeight: 1.4,
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {children}
    </span>
  );
}

// ────────────────────────────────────────────────────────────
// Copy button — shows momentary "Copied"
// ────────────────────────────────────────────────────────────
function CopyButton({ text, label = 'Copy', size = 'sm' }) {
  const [copied, setCopied] = useState(false);
  const onCopy = useCallback(() => {
    try {
      navigator.clipboard?.writeText(text);
    } catch (e) {}
    setCopied(true);
    setTimeout(() => setCopied(false), 1400);
  }, [text]);

  const padding = size === 'sm' ? '4px 8px' : '6px 10px';
  const fontSize = size === 'sm' ? 11 : 12;

  return (
    <button
      onClick={onCopy}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding,
        fontSize,
        fontWeight: 500,
        color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
        background: 'transparent',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-tag)',
        transition: 'all 120ms ease',
      }}
      onMouseEnter={e => { e.currentTarget.style.borderColor = 'var(--border-strong)'; e.currentTarget.style.color = 'var(--text)'; }}
      onMouseLeave={e => { e.currentTarget.style.borderColor = 'var(--border)'; e.currentTarget.style.color = copied ? 'var(--ok-text)' : 'var(--text-muted)'; }}
    >
      <svg width="11" height="11" viewBox="0 0 12 12" fill="none">
        {copied ? (
          <path d="M2.5 6.2L4.7 8.5L9.5 3.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
        ) : (
          <>
            <rect x="3.5" y="3.5" width="6" height="6" rx="1" stroke="currentColor" strokeWidth="1" />
            <path d="M2 7.5V2.5C2 2.22 2.22 2 2.5 2H7.5" stroke="currentColor" strokeWidth="1" strokeLinecap="round" />
          </>
        )}
      </svg>
      <span>{copied ? 'Copied' : label}</span>
    </button>
  );
}

// ────────────────────────────────────────────────────────────
// Segmented control — for time range etc.
// ────────────────────────────────────────────────────────────
function Segmented({ options, value, onChange, size = 'md' }) {
  const padding = size === 'sm' ? '3px 8px' : '5px 12px';
  const fontSize = size === 'sm' ? 11 : 12;

  return (
    <div style={{
      display: 'inline-flex',
      padding: 2,
      background: 'var(--bg-soft)',
      border: '0.5px solid var(--border)',
      borderRadius: 'var(--r-tag)',
      gap: 2,
    }}>
      {options.map(opt => {
        const active = opt.value === value;
        return (
          <button
            key={opt.value}
            onClick={() => onChange(opt.value)}
            style={{
              padding,
              fontSize,
              fontWeight: 500,
              fontFamily: opt.mono ? 'var(--font-mono)' : 'var(--font-sans)',
              letterSpacing: opt.mono ? '-0.02em' : 0,
              color: active ? 'var(--text)' : 'var(--text-muted)',
              background: active ? 'var(--bg-card)' : 'transparent',
              border: active ? '0.5px solid var(--border)' : '0.5px solid transparent',
              borderRadius: 4,
              transition: 'all 120ms ease',
              whiteSpace: 'nowrap',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Code block — with header bar (file + Copy)
// ────────────────────────────────────────────────────────────
function highlight(text, lang) {
  // lightweight tokenization — variables {VAR}, comments, strings
  const out = [];
  let i = 0;
  const push = (type, value) => out.push({ type, value });

  // line-by-line for comment detection
  const lines = text.split('\n');
  lines.forEach((line, lineIdx) => {
    let rest = line;
    // detect comment start
    const commentMatch = rest.match(/^(\s*)(#|\/\/|;)(.*)$/);
    const xmlComment = rest.match(/^(\s*)(<!--.*-->)(.*)$/);
    if (commentMatch) {
      push('plain', commentMatch[1]);
      push('comment', commentMatch[2] + commentMatch[3]);
    } else if (xmlComment) {
      push('plain', xmlComment[1]);
      push('comment', xmlComment[2]);
      push('plain', xmlComment[3]);
    } else {
      // walk char-by-char
      let buf = '';
      let j = 0;
      while (j < rest.length) {
        const ch = rest[j];
        // {VAR} placeholder
        if (ch === '{' && /[A-Z]/.test(rest[j + 1] || '')) {
          const end = rest.indexOf('}', j);
          if (end > -1) {
            if (buf) { push('plain', buf); buf = ''; }
            push('var', rest.slice(j, end + 1));
            j = end + 1;
            continue;
          }
        }
        // strings  "..."  '...'
        if (ch === '"' || ch === "'") {
          const quote = ch;
          let end = j + 1;
          while (end < rest.length && rest[end] !== quote) end++;
          if (rest[end] === quote) {
            if (buf) { push('plain', buf); buf = ''; }
            push('str', rest.slice(j, end + 1));
            j = end + 1;
            continue;
          }
        }
        buf += ch;
        j++;
      }
      if (buf) push('plain', buf);
    }
    if (lineIdx < lines.length - 1) push('plain', '\n');
  });

  return out;
}

function CodeBlock({ file, body, lang = 'sh', accentLang = false }) {
  const tokens = useMemo(() => highlight(body, lang), [body, lang]);
  return (
    <div style={{
      background: 'var(--bg-soft)',
      border: '0.5px solid var(--border)',
      borderRadius: 8,
      overflow: 'hidden',
      fontFamily: 'var(--font-mono)',
    }}>
      <div style={{
        height: 28,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '0 8px 0 12px',
        borderBottom: '0.5px solid var(--border)',
        background: 'var(--bg-card)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
          {file && (
            <span style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              color: 'var(--text-muted)',
              letterSpacing: '-0.01em',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}>
              {file}
            </span>
          )}
          {accentLang && (
            <span className="eyebrow">{lang}</span>
          )}
        </div>
        <CopyButton text={body} />
      </div>
      <pre style={{
        margin: 0,
        padding: '12px 14px',
        fontSize: 12,
        lineHeight: 1.6,
        color: 'var(--text)',
        overflowX: 'auto',
        whiteSpace: 'pre',
      }}>
        {tokens.map((t, i) => {
          if (t.type === 'comment') return <span key={i} style={{ color: 'var(--text-subtle)', fontStyle: 'italic' }}>{t.value}</span>;
          if (t.type === 'var')     return <span key={i} style={{ color: 'var(--brand)', fontWeight: 500 }}>{t.value}</span>;
          if (t.type === 'str')     return <span key={i} style={{ color: 'var(--ok-text)' }}>{t.value}</span>;
          return <span key={i}>{t.value}</span>;
        })}
      </pre>
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Logo glyph — original mark for Depsilo
// Concentric rings on stem suggest "cache depths"
// ────────────────────────────────────────────────────────────
function Logo({ size = 18 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 20 20" fill="none">
      <defs>
        <linearGradient id="logo-grad" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="oklch(0.50 0.20 305)" />
          <stop offset="60%" stopColor="oklch(0.56 0.16 270)" />
          <stop offset="100%" stopColor="oklch(0.66 0.13 220)" />
        </linearGradient>
      </defs>
      <rect x="1" y="1" width="18" height="18" rx="5" fill="url(#logo-grad)" />
      <circle cx="10" cy="10" r="5.5" fill="none" stroke="#fff" strokeWidth="1.2" opacity="0.4" />
      <circle cx="10" cy="10" r="3.2" fill="none" stroke="#fff" strokeWidth="1.2" opacity="0.7" />
      <circle cx="10" cy="10" r="1.2" fill="#fff" />
    </svg>
  );
}

// ────────────────────────────────────────────────────────────
// Manager glyph — monogram chip (placeholder, brand-neutral)
// ────────────────────────────────────────────────────────────
function ManagerGlyph({ glyph, selected = false, size = 32 }) {
  return (
    <div style={{
      width: size,
      height: size,
      borderRadius: 8,
      background: selected ? 'var(--brand-soft)' : 'var(--bg-soft)',
      border: `0.5px solid ${selected ? 'var(--brand-border)' : 'var(--border)'}`,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      fontFamily: 'var(--font-mono)',
      fontSize: 10,
      fontWeight: 500,
      letterSpacing: '-0.02em',
      color: selected ? 'var(--brand)' : 'var(--text-muted)',
      transition: 'all 140ms ease',
    }}>
      {glyph}
    </div>
  );
}

// expose to global scope for other babel scripts
Object.assign(window, {
  Sparkline, StatusDot, Tag, CopyButton, Segmented, CodeBlock, Logo, ManagerGlyph,
});
