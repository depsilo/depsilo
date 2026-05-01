/* ============================================================
   Depsilo — Monitoring page (redesigned)
   Composition: hero hit-rate + KPI strip + full-width latency
   chart + 2-column mirror matrix.
   ============================================================ */

// ────────────────────────────────────────────────────────────
// Hero hit-rate — large numeric + bold sparkline
// ────────────────────────────────────────────────────────────
function HitRateHero({ kpi }) {
  return (
    <div className="card aurora-rim" style={{
      padding: '28px 32px',
      display: 'grid',
      gridTemplateColumns: '320px 1fr',
      alignItems: 'stretch',
      gap: 32,
      minHeight: 168,
      position: 'relative',
      overflow: 'hidden',
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <div>
          <div className="eyebrow" style={{
            display: 'flex', alignItems: 'center', gap: 8,
          }}>
            <StatusDot status="healthy" live />
            <span>Cache hit rate · 1h</span>
          </div>
          <div className="aurora-glow" style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 14 }}>
            <span className="grad-text-aurora" style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 76, fontWeight: 600, letterSpacing: '-0.06em',
              lineHeight: 1,
              fontVariantNumeric: 'tabular-nums',
            }}>94.2</span>
            <span style={{ fontSize: 22, color: 'var(--text-soft)' }}>%</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 12 }}>
            <span style={{
              fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: 600,
              letterSpacing: '-0.01em',
              color: 'var(--ok-text)',
              padding: '2px 6px',
              background: 'var(--ok-fill)',
              border: '0.5px solid var(--ok-border)',
              borderRadius: 4,
              whiteSpace: 'nowrap',
            }}>+1.4 pts</span>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>vs. 24h baseline</span>
          </div>
        </div>
      </div>
      <div style={{
        display: 'flex',
        alignItems: 'flex-end',
        position: 'relative',
        borderLeft: '0.5px solid var(--border)',
        paddingLeft: 32,
      }}>
        <HeroSparkline values={kpi.series} />
      </div>
    </div>
  );
}

function HeroSparkline({ values }) {
  // larger format with subtle gridlines + axis labels
  const W = 760, H = 110, padY = 6;
  if (!values?.length) return null;
  const min = Math.min(...values), max = Math.max(...values);
  const range = (max - min) || 1;
  const points = values.map((v, i) => {
    const x = (i / (values.length - 1)) * W;
    const y = padY + (1 - (v - min) / range) * (H - padY * 2);
    return [x, y];
  });
  const linePath = points.map((p, i) => (i === 0 ? `M${p[0]},${p[1]}` : `L${p[0]},${p[1]}`)).join(' ');
  const areaPath = `${linePath} L${W},${H} L0,${H} Z`;
  const last = points[points.length - 1];

  return (
    <svg viewBox={`0 0 ${W} ${H + 16}`} preserveAspectRatio="none" style={{ width: '100%', height: 110, overflow: 'visible' }}>
      <defs>
        <linearGradient id="hero-stroke" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%" stopColor="var(--spec-1)" />
          <stop offset="55%" stopColor="var(--spec-2)" />
          <stop offset="100%" stopColor="var(--spec-3)" />
        </linearGradient>
        <linearGradient id="hero-area" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="oklch(0.62 0.18 305)" stopOpacity="0.18" />
          <stop offset="100%" stopColor="oklch(0.72 0.12 210)" stopOpacity="0" />
        </linearGradient>
      </defs>
      {/* gridlines */}
      {[0.25, 0.5, 0.75].map(g => (
        <line key={g} x1="0" x2={W} y1={H * g} y2={H * g} stroke="var(--border)" strokeWidth="0.5" strokeDasharray="2 3" />
      ))}
      <path d={areaPath} fill="url(#hero-area)" />
      <path d={linePath} fill="none" stroke="url(#hero-stroke)" strokeWidth="1.4" strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={last[0]} cy={last[1]} r="3" fill="var(--spec-2)" />
      <circle cx={last[0]} cy={last[1]} r="7" fill="var(--spec-2)" opacity="0.18" />
      {/* x labels */}
      {['−60m', '−45m', '−30m', '−15m', 'now'].map((l, i) => (
        <text key={l} x={(i / 4) * W} y={H + 12}
          fontFamily="var(--font-mono)" fontSize="9"
          fill="var(--text-subtle)"
          textAnchor={i === 0 ? 'start' : i === 4 ? 'end' : 'middle'}>{l}</text>
      ))}
    </svg>
  );
}

// ────────────────────────────────────────────────────────────
// Stat strip — 3 supporting metrics, single row, dividers
// ────────────────────────────────────────────────────────────
function StatStrip() {
  const items = [
    { label: 'Total requests', value: '1.28', unit: 'M', delta: '+12%', sub: '24h', tone: 'brand', series: window.DEPSILO_DATA.KPIS[1].series },
    { label: 'Bandwidth saved', value: '847', unit: 'GB', delta: '+96 GB', sub: '24h', tone: 'brand', series: window.DEPSILO_DATA.KPIS[2].series },
    { label: 'P50 latency', value: '42', unit: 'ms', delta: '−3 ms', sub: 'mean', tone: 'neutral', series: window.DEPSILO_DATA.KPIS[3].series },
  ];
  return (
    <div className="card" style={{
      display: 'grid',
      gridTemplateColumns: 'repeat(3, 1fr)',
      gap: 0,
      padding: 0,
    }}>
      {items.map((it, i) => (
        <div key={it.label} style={{
          padding: '16px 20px',
          borderRight: i < items.length - 1 ? '0.5px solid var(--border)' : 'none',
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{it.label}</span>
            <span style={{
              fontFamily: 'var(--font-mono)', fontSize: 11,
              color: 'var(--ok-text)',
              whiteSpace: 'nowrap',
            }}>{it.delta}</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8 }}>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 4 }}>
              <span style={{ fontFamily: 'var(--font-mono)', fontSize: 32, fontWeight: 600, letterSpacing: '-0.04em', fontVariantNumeric: 'tabular-nums' }}>{it.value}</span>
              <span style={{ fontSize: 13, color: 'var(--text-soft)' }}>{it.unit}</span>
            </div>
            <Sparkline values={it.series} width={120} height={26} tone={it.tone} />
          </div>
        </div>
      ))}
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Mirror matrix — 2 columns of compact cards (denser than table)
// ────────────────────────────────────────────────────────────
function MirrorTile({ mirror }) {
  const tone = mirror.status === 'healthy' ? 'ok' : mirror.status === 'degraded' ? 'warn' : 'danger';
  const isFailed = mirror.status === 'failed';
  return (
    <div className="row-hover" style={{
      display: 'grid',
      gridTemplateColumns: '50px 1fr 96px',
      alignItems: 'center',
      gap: 12,
      padding: '10px 14px',
      borderBottom: '0.5px solid var(--border)',
      cursor: 'pointer',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot status={mirror.status} />
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 11,
          color: 'var(--text)',
        }}>{mirror.type}</span>
      </div>
      <div style={{ minWidth: 0 }}>
        <div className="mono" style={{
          fontSize: 12,
          color: isFailed ? 'var(--text-subtle)' : 'var(--text)',
          textDecoration: isFailed ? 'line-through' : 'none',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}>{mirror.url.replace(/^https?:\/\//, '')}</div>
        <div style={{ display: 'flex', gap: 14, marginTop: 2, fontSize: 11 }}>
          <span style={{ color: 'var(--text-subtle)' }}>P50 <span className="num" style={{
            color: isFailed ? 'var(--text-subtle)' : mirror.p50 > 100 ? 'var(--warn-text)' : 'var(--text-muted)',
          }}>{isFailed ? '—' : `${mirror.p50}ms`}</span></span>
          <span style={{ color: 'var(--text-subtle)' }}>hit <span className="num" style={{
            color: isFailed ? 'var(--text-subtle)' : 'var(--text-muted)',
          }}>{mirror.hit}</span></span>
        </div>
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Sparkline values={mirror.series} width={92} height={22} tone={tone} showDot={!isFailed} />
      </div>
    </div>
  );
}

function MirrorMatrix({ mirrors }) {
  // split into two columns
  const half = Math.ceil(mirrors.length / 2);
  const left = mirrors.slice(0, half);
  const right = mirrors.slice(half);
  return (
    <div className="card" style={{
      display: 'grid',
      gridTemplateColumns: '1fr 1fr',
      overflow: 'hidden',
    }}>
      <div style={{ borderRight: '0.5px solid var(--border)' }}>
        {left.map((m, i) => (
          <div key={m.type} style={{ borderBottom: i === left.length - 1 ? 'none' : null }}>
            <MirrorTile mirror={m} />
          </div>
        ))}
      </div>
      <div>
        {right.map((m, i) => (
          <div key={m.type} style={{ borderBottom: i === right.length - 1 ? 'none' : null }}>
            <MirrorTile mirror={m} />
          </div>
        ))}
      </div>
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Banner
// ────────────────────────────────────────────────────────────
function Banner({ children, onAction, actionLabel = 'Reconnect', onDismiss }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 12,
      padding: '8px 12px',
      background: 'var(--warn-fill)',
      border: '0.5px solid var(--warn-border)',
      borderRadius: 8,
      fontSize: 12,
    }}>
      <StatusDot status="degraded" />
      <span style={{ flex: 1, color: 'var(--text)' }}>{children}</span>
      {onAction && (
        <button onClick={onAction} style={{
          padding: '3px 10px', fontSize: 11, fontWeight: 500,
          color: 'var(--warn-text)',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--warn-border)',
          borderRadius: 6,
        }}>{actionLabel}</button>
      )}
      {onDismiss && (
        <button onClick={onDismiss} style={{ color: 'var(--text-muted)', padding: 4 }}>
          <svg width="10" height="10" viewBox="0 0 10 10"><path d="M2 2l6 6M8 2l-6 6" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round"/></svg>
        </button>
      )}
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Page
// ────────────────────────────────────────────────────────────
function MonitorPage({ timeRange, setTimeRange, daemonStatus }) {
  const [showBanner, setShowBanner] = useState(daemonStatus !== 'connected');
  useEffect(() => { setShowBanner(daemonStatus !== 'connected'); }, [daemonStatus]);

  const ranges = [
    { value: '15m', label: '15m', mono: true },
    { value: '1h',  label: '1h',  mono: true },
    { value: '24h', label: '24h', mono: true },
    { value: '7d',  label: '7d',  mono: true },
  ];

  const counts = window.DEPSILO_DATA.MIRRORS.reduce((acc, m) => {
    acc[m.status] = (acc[m.status] || 0) + 1; return acc;
  }, {});

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Title row */}
      <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <h1 className="grad-text" style={{
            margin: 0, fontSize: 44, fontWeight: 700,
            letterSpacing: '-0.04em', lineHeight: 1.02,
          }}>Live monitoring</h1>
          <p style={{ margin: '14px 0 0 0', fontSize: 17, lineHeight: 1.45, color: 'var(--text)', maxWidth: 620, fontWeight: 400, letterSpacing: '-0.005em' }}>
            Real-time view of cache performance and upstream mirror health.
            <span className="mono" style={{ marginLeft: 10, color: 'var(--text-subtle)', fontSize: 12, fontWeight: 500 }}>
              · updated 2s ago
            </span>
          </p>
        </div>
        <Segmented options={ranges} value={timeRange} onChange={setTimeRange} />
      </div>

      {showBanner && (
        <Banner onAction={() => setShowBanner(false)} onDismiss={() => setShowBanner(false)}>
          Upstream <span className="mono" style={{ color: 'var(--text)' }}>registry-1.docker.io</span> response time is elevated (P50 184ms). Falling back to secondary mirror.
        </Banner>
      )}

      {/* Hero hit rate */}
      <HitRateHero kpi={window.DEPSILO_DATA.KPIS[0]} />

      {/* Stat strip */}
      <StatStrip />

      {/* Mirrors */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between' }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 26, fontWeight: 700, letterSpacing: '-0.03em', lineHeight: 1.1 }}>
              Upstream mirrors <span style={{
                fontFamily: 'var(--font-mono)', fontSize: 14,
                fontWeight: 500, letterSpacing: '-0.02em',
                color: 'var(--text-subtle)', marginLeft: 6,
              }}>{window.DEPSILO_DATA.MIRRORS.length}</span>
            </h2>
            <p style={{ margin: '2px 0 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="healthy" /> <span className="num">{counts.healthy || 0}</span> healthy
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="degraded" /> <span className="num">{counts.degraded || 0}</span> degraded
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="failed" /> <span className="num">{counts.failed || 0}</span> failed
              </span>
            </p>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <button style={{
              fontSize: 12, fontWeight: 500, color: 'var(--text-muted)',
              padding: '5px 10px', border: '0.5px solid var(--border)', borderRadius: 6,
            }}>Filter</button>
            <button style={{
              fontSize: 12, fontWeight: 500, color: 'var(--text-muted)',
              padding: '5px 10px', border: '0.5px solid var(--border)', borderRadius: 6,
            }}>Add mirror</button>
          </div>
        </div>
        <MirrorMatrix mirrors={window.DEPSILO_DATA.MIRRORS} />
      </div>
    </div>
  );
}

window.MonitorPage = MonitorPage;
