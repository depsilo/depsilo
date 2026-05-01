/* ============================================================
   Depsilo — Top navigation (slim, only Monitoring + Quick start)
   ============================================================ */

const navItems = [
  { id: 'quickstart', label: 'Quick start' },
  { id: 'monitor', label: 'Monitoring' },
];

function TopNav({ tab, setTab, theme, setTheme, lang, setLang, daemonStatus }) {
  const statusMap = {
    connected: { label: 'Connected', dot: 'healthy' },
    degraded:  { label: 'Degraded',  dot: 'degraded' },
    offline:   { label: 'Offline',   dot: 'failed' },
  };
  const s = statusMap[daemonStatus] || statusMap.connected;

  return (
    <header style={{
      position: 'sticky', top: 0, zIndex: 30,
      background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
      backdropFilter: 'saturate(180%) blur(8px)',
      WebkitBackdropFilter: 'saturate(180%) blur(8px)',
      borderBottom: '0.5px solid var(--border)',
    }}>
      <div style={{
        height: 52, maxWidth: 1240, margin: '0 auto',
        padding: '0 28px',
        display: 'flex', alignItems: 'center', gap: 24,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Logo />
          <span style={{ fontSize: 15, fontWeight: 700, letterSpacing: '-0.025em' }}>depsilo</span>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 10,
            color: 'var(--text-subtle)', padding: '1px 5px',
            border: '0.5px solid var(--border)', borderRadius: 4, marginLeft: 2,
          }}>v2.4.1</span>
        </div>

        <nav style={{ display: 'flex', alignItems: 'center', gap: 2, marginLeft: 8 }}>
          {navItems.map(n => {
            const active = tab === n.id;
            return (
              <button
                key={n.id}
                onClick={() => setTab(n.id)}
                style={{
                  position: 'relative', padding: '6px 10px',
                  fontSize: 13, fontWeight: active ? 600 : 500,
                  letterSpacing: active ? '-0.005em' : 0,
                  color: active ? 'var(--text)' : 'var(--text-soft)',
                  borderRadius: 6, transition: 'color 120ms ease, font-weight 120ms ease',
                }}>
                {n.label}
                {active && (
                  <span style={{
                    position: 'absolute', left: 10, right: 10, bottom: -15,
                    height: 1.5, background: 'var(--grad-brand)', borderRadius: 1,
                  }}/>
                )}
              </button>
            );
          })}
        </nav>

        <div style={{ flex: 1 }}/>

        <div style={{
          display: 'inline-flex', alignItems: 'center', gap: 6,
          padding: '4px 10px 4px 8px',
          fontSize: 11, fontWeight: 600,
          letterSpacing: '0.005em',
          color: 'var(--text)',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 999,
          whiteSpace: 'nowrap',
        }}>
          <StatusDot status={s.dot} live size={6} />
          <span>{s.label}</span>
          <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-subtle)' }}>· :8080</span>
        </div>

        <button
          onClick={() => setLang(lang === 'en' ? 'zh' : 'en')}
          style={{
            fontSize: 11, fontWeight: 500, padding: '4px 8px',
            color: 'var(--text-muted)',
            border: '0.5px solid var(--border)', borderRadius: 6,
            fontFamily: 'var(--font-mono)',
          }}>{lang === 'en' ? 'EN' : '中'}</button>

        <button
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          style={{
            width: 28, height: 28,
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            color: 'var(--text-muted)',
            border: '0.5px solid var(--border)', borderRadius: 6,
          }}>
          {theme === 'dark' ? (
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.2"/>
              <g stroke="currentColor" strokeWidth="1.2" strokeLinecap="round">
                <path d="M8 1.5v1.5M8 13v1.5M14.5 8H13M3 8H1.5M12.6 3.4l-1 1M4.4 11.6l-1 1M12.6 12.6l-1-1M4.4 4.4l-1-1"/>
              </g>
            </svg>
          ) : (
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
              <path d="M13 9.5A5.5 5.5 0 016.5 3a5.5 5.5 0 106.5 6.5z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round"/>
            </svg>
          )}
        </button>

        <button style={{
          display: 'inline-flex', alignItems: 'center', gap: 8,
          padding: '4px 10px 4px 4px',
          color: 'var(--text)',
          border: '0.5px solid var(--border)', borderRadius: 999,
          fontSize: 12, fontWeight: 500,
        }}>
          <span style={{
            width: 22, height: 22, borderRadius: '50%',
            background: 'var(--brand-soft)',
            border: '0.5px solid var(--brand-border)',
            color: 'var(--brand)',
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 10, fontWeight: 500, fontFamily: 'var(--font-mono)',
          }}>DV</span>
          <span>Admin</span>
        </button>
      </div>
    </header>
  );
}

window.TopNav = TopNav;
