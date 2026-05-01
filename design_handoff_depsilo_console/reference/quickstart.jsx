/* ============================================================
   Depsilo — Quick Start
   • Left rail: languages (no scroll) + "All-in-one" feature row
   • Right pane: managers within the language as variant tabs
   • Each manager has ONE canonical config (no perm/once split)
   • AI prompt drawer for one-click copy
   ============================================================ */

function EndpointInline({ endpoint }) {
  return (
    <div style={{
      display: 'inline-flex', alignItems: 'center', gap: 8,
      padding: '4px 6px 4px 10px',
      background: 'var(--bg-soft)',
      border: '0.5px solid var(--border)',
      borderRadius: 6,
      flexShrink: 0,
    }}>
      <StatusDot status="healthy" live />
      <span className="mono" style={{
        fontSize: 12, color: 'var(--text)', letterSpacing: '-0.02em',
        whiteSpace: 'nowrap',
      }}>{endpoint}</span>
      <CopyButton text={endpoint} />
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Left rail — languages (no scroll)
// ────────────────────────────────────────────────────────────
function LanguageRail({ selected, onSelect }) {
  const langs = window.DEPSILO_DATA.LANGUAGES;
  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}>
      {/* All-in-one feature row */}
      <button
        onClick={() => onSelect('all')}
        style={{
          display: 'flex', alignItems: 'center', gap: 10,
          padding: '10px 12px',
          background: selected === 'all' ? 'var(--brand-soft)' : 'transparent',
          borderBottom: '0.5px solid var(--border)',
          borderLeft: selected === 'all' ? '2px solid var(--brand)' : '2px solid transparent',
          textAlign: 'left',
          cursor: 'pointer',
        }}
        onMouseEnter={e => { if (selected !== 'all') e.currentTarget.style.background = 'var(--bg-hover)'; }}
        onMouseLeave={e => { if (selected !== 'all') e.currentTarget.style.background = 'transparent'; }}
      >
        <div style={{
          width: 26, height: 26, borderRadius: 6,
          background: 'var(--brand)',
          display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
          color: '#fff', flexShrink: 0,
        }}>
          <svg width="13" height="13" viewBox="0 0 14 14" fill="none">
            <path d="M2.5 4l2 2 4-4M2.5 9l2 2 4-4M11 6h.5M11 11h.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"/>
          </svg>
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{
            fontSize: 12, fontWeight: 500,
            color: selected === 'all' ? 'var(--brand)' : 'var(--text)',
            whiteSpace: 'nowrap',
          }}>All-in-one</div>
          <div style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>Configure everything</div>
        </div>
      </button>

      {/* divider + section label */}
      <div style={{
        padding: '8px 12px 4px',
        borderBottom: '0.5px solid var(--border)',
      }}>
        <span className="eyebrow">Or by language</span>
      </div>

      {/* Language list — fills remaining space, no scroll */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
        {langs.map((l, i) => {
          const active = l.id === selected;
          return (
            <button
              key={l.id}
              onClick={() => onSelect(l.id)}
              style={{
                flex: 1,
                display: 'flex', alignItems: 'center', gap: 10,
                padding: '0 12px',
                textAlign: 'left',
                background: active ? 'var(--brand-soft)' : 'transparent',
                borderLeft: active ? '2px solid var(--brand)' : '2px solid transparent',
                borderBottom: i === langs.length - 1 ? 'none' : '0.5px solid var(--border)',
                transition: 'background 100ms ease',
                cursor: 'pointer',
                minHeight: 0,
              }}
              onMouseEnter={e => { if (!active) e.currentTarget.style.background = 'var(--bg-hover)'; }}
              onMouseLeave={e => { if (!active) e.currentTarget.style.background = 'transparent'; }}
            >
              <span style={{
                width: 22, height: 22, borderRadius: 4,
                background: 'transparent',
                display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 500,
                color: active ? 'var(--brand)' : 'var(--text-subtle)',
                flexShrink: 0,
              }}>{l.glyph}</span>
              <span style={{
                fontSize: 12.5, fontWeight: active ? 500 : 400,
                color: active ? 'var(--brand)' : 'var(--text)',
                flex: 1, minWidth: 0, whiteSpace: 'nowrap',
                overflow: 'hidden', textOverflow: 'ellipsis',
              }}>{l.name}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Section helpers
// ────────────────────────────────────────────────────────────

function ConfigSection({ step, title, subtitle, children }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, padding: '0 2px' }}>
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 11,
          color: 'var(--text-subtle)',
          letterSpacing: '0.04em',
          flexShrink: 0,
        }}>{String(step).padStart(2, '0')}</span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)', letterSpacing: '-0.005em' }}>{title}</div>
          {subtitle && <div style={{ fontSize: 12, color: 'var(--text-soft)', marginTop: 2 }}>{subtitle}</div>}
        </div>
      </div>
      {children}
    </div>
  );
}

function ShellSnippet({ body, url, host, muted = false }) {
  return (
    <div style={{
      background: muted ? 'var(--bg-soft)' : 'var(--bg-soft)',
      border: '0.5px solid var(--border)',
      borderRadius: 8, overflow: 'hidden',
    }}>
      <div style={{
        height: 28, display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '0 8px 0 12px',
        borderBottom: '0.5px solid var(--border)',
        background: 'var(--bg-card)',
      }}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>
          shell
        </span>
        <CopyButton text={body} />
      </div>
      <pre style={{
        margin: 0, padding: '12px 14px',
        fontSize: 12, lineHeight: 1.6,
        fontFamily: 'var(--font-mono)',
        overflowX: 'auto', whiteSpace: 'pre',
      }}>
        {body.split('\n').map((line, i) => (
          <div key={i}>
            <span style={{ color: 'var(--text-subtle)' }}>$ </span>
            {fillWithMarkup(line, url, host)}
          </div>
        ))}
      </pre>
    </div>
  );
}

function fillWithMarkup(s, url, host) {
  const replaced = s.replace(/\{URL\}/g, url).replace(/\{HOST\}/g, host);
  const parts = []; let buf = '';
  for (let i = 0; i < replaced.length; i++) {
    const ch = replaced[i];
    if (ch === '<') {
      const end = replaced.indexOf('>', i);
      if (end > -1) {
        if (buf) { parts.push({ t: 'p', v: buf }); buf = ''; }
        parts.push({ t: 'v', v: replaced.slice(i, end + 1) });
        i = end; continue;
      }
    }
    buf += ch;
  }
  if (buf) parts.push({ t: 'p', v: buf });
  return parts.map((p, i) => p.t === 'v'
    ? <span key={i} style={{ color: 'var(--text-muted)', fontStyle: 'italic' }}>{p.v}</span>
    : <span key={i}>{p.v}</span>);
}

// ────────────────────────────────────────────────────────────
// Manager tabs (right pane top)
// ────────────────────────────────────────────────────────────
function ManagerTabs({ managers, active, onChange }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap' }}>
      {managers.map(m => {
        const isActive = m.id === active;
        return (
          <button
            key={m.id}
            onClick={() => onChange(m.id)}
            style={{
              display: 'inline-flex', flexDirection: 'column',
              padding: '6px 10px',
              background: isActive ? 'var(--bg-card)' : 'transparent',
              border: `0.5px solid ${isActive ? 'var(--border-strong)' : 'var(--border)'}`,
              borderRadius: 6,
              textAlign: 'left',
              transition: 'all 120ms ease',
            }}
          >
            <span style={{
              fontSize: 12, fontWeight: 500,
              color: isActive ? 'var(--text)' : 'var(--text-muted)',
              whiteSpace: 'nowrap',
            }}>{m.name}</span>
            <span style={{
              fontSize: 10, color: 'var(--text-subtle)',
              whiteSpace: 'nowrap',
            }}>{m.hint}</span>
          </button>
        );
      })}
    </div>
  );
}

function PromptCard({ prompt, target = 'AI assistant' }) {
  return (
    <div style={{
      background: 'var(--bg-soft)',
      border: '0.5px solid var(--border)',
      borderRadius: 8, overflow: 'hidden',
    }}>
      <div style={{
        height: 32,
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '0 8px 0 12px',
        borderBottom: '0.5px solid var(--border)',
        background: 'var(--bg-card)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, whiteSpace: 'nowrap' }}>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 10,
            color: 'var(--brand)',
            padding: '1px 6px',
            background: 'var(--brand-soft)',
            borderRadius: 3,
            letterSpacing: '0.04em',
          }}>AI</span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Prompt for {target}</span>
        </div>
        <CopyButton text={prompt} label="Copy" />
      </div>
      <div style={{
        padding: '12px 14px',
        fontSize: 12, lineHeight: 1.6,
        color: 'var(--text)', whiteSpace: 'pre-wrap',
        maxHeight: 180, overflowY: 'auto',
      }}>{prompt}</div>
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// All-in-one pane
// ────────────────────────────────────────────────────────────
function AllInOnePane({ endpoint }) {
  const [mode, setMode] = useState('script');
  const script = window.DEPSILO_DATA.buildAllScript(endpoint);
  const prompt = window.DEPSILO_DATA.buildPrompt(endpoint, 'all');

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}>
      <div style={{
        display: 'flex', alignItems: 'center',
        borderBottom: '0.5px solid var(--border)',
        padding: '0 14px', height: 44, flexShrink: 0, gap: 12,
      }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 17, fontWeight: 700, whiteSpace: 'nowrap', letterSpacing: '-0.02em', lineHeight: 1.2 }}>All-in-one setup</div>
          <div style={{ fontSize: 12, color: 'var(--text-soft)', whiteSpace: 'nowrap', marginTop: 2 }}>Configure every detected package manager in one go</div>
        </div>
        <Segmented
          options={[
            { value: 'script', label: 'Shell script' },
            { value: 'prompt', label: 'AI prompt' },
          ]}
          value={mode} onChange={setMode}
        />
      </div>

      <div style={{ padding: 16, flex: 1, overflow: 'auto', minHeight: 0, display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {mode === 'script'
            ? 'Run this as root on your machine. It edits config for pip, npm, cargo, go, and docker — extend as needed.'
            : 'Paste this into ChatGPT, Claude, Cursor, or any agentic coding tool. The AI will detect your stack and edit the right files.'}
        </div>
        {mode === 'script' ? (
          <CodeBlock file="depsilo-setup.sh" body={script} lang="sh" accentLang />
        ) : (
          <PromptCard prompt={prompt} target="any AI coding tool" />
        )}
      </div>
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Per-language pane (managers as tabs)
// ────────────────────────────────────────────────────────────
function ConfigurePane({ language, endpoint }) {
  const lang = window.DEPSILO_DATA.LANGUAGES.find(l => l.id === language);
  const [mgrId, setMgrId] = useState(lang?.managers[0]?.id);
  const [showPrompt, setShowPrompt] = useState(false);

  useEffect(() => { setMgrId(lang?.managers[0]?.id); setShowPrompt(false); }, [language]);
  if (!lang) return null;

  const m = lang.managers.find(x => x.id === mgrId) || lang.managers[0];
  const url = endpoint;
  const host = url.replace(/^https?:\/\//, '');
  const fill = (s) => (s || '').replace(/\{URL\}/g, url).replace(/\{HOST\}/g, host);
  const prompt = window.DEPSILO_DATA.buildPrompt(endpoint, language);

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}>
      <div style={{
        display: 'flex', alignItems: 'center',
        borderBottom: '0.5px solid var(--border)',
        padding: '0 14px', height: 44, flexShrink: 0, gap: 12,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, whiteSpace: 'nowrap' }}>
          <span style={{
            width: 22, height: 22, borderRadius: 5,
            background: 'var(--brand-soft)',
            border: '0.5px solid var(--brand-border)',
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 500,
            color: 'var(--brand)',
          }}>{lang.glyph}</span>
          <span style={{ fontSize: 17, fontWeight: 700, letterSpacing: '-0.02em' }}>Configure {lang.name}</span>
          <span style={{ fontSize: 10, color: 'var(--text-subtle)', marginLeft: 4 }}>
            {lang.managers.length} {lang.managers.length === 1 ? 'manager' : 'managers'}
          </span>
        </div>
        <div style={{ flex: 1 }}/>
        <button
          onClick={() => setShowPrompt(p => !p)}
          style={{
            display: 'inline-flex', alignItems: 'center', gap: 6,
            padding: '4px 10px',
            fontSize: 11, fontWeight: 500,
            color: showPrompt ? 'var(--brand)' : 'var(--text-muted)',
            background: showPrompt ? 'var(--brand-soft)' : 'transparent',
            border: `0.5px solid ${showPrompt ? 'var(--brand-border)' : 'var(--border)'}`,
            borderRadius: 6, whiteSpace: 'nowrap',
          }}
        >
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9,
            color: showPrompt ? 'var(--brand)' : 'var(--text-subtle)',
            letterSpacing: '0.04em',
          }}>AI</span>
          prompt
        </button>
      </div>

      <div style={{ padding: 16, flex: 1, overflow: 'auto', minHeight: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
        {showPrompt && <PromptCard prompt={prompt} target="ChatGPT / Claude / Cursor" />}

        {lang.managers.length > 1 && (
          <ManagerTabs managers={lang.managers} active={m.id} onChange={setMgrId} />
        )}

        {/* 01 — Configure (persistent + paths) */}
        <ConfigSection
          step={1}
          title="Configure"
          subtitle={`Edit ${m.persistent.file} — applied to every install from now on`}
        >
          <CodeBlock file={m.persistent.file} body={fill(m.persistent.body)} lang={m.persistent.lang} accentLang />
          <details style={{
            marginTop: 8,
            border: '0.5px solid var(--border)',
            borderRadius: 6,
            background: 'var(--bg-soft)',
          }}>
            <summary style={{
              padding: '6px 12px',
              fontSize: 11, color: 'var(--text-muted)',
              cursor: 'pointer',
              display: 'flex', alignItems: 'center', gap: 6,
              listStyle: 'none',
            }}>
              <span style={{ color: 'var(--text-subtle)' }}>▸</span>
              Where this manager reads config from
            </summary>
            <div style={{ borderTop: '0.5px solid var(--border)' }}>
              {m.paths.map((p, i) => (
                <div key={i} style={{
                  display: 'grid',
                  gridTemplateColumns: '120px 1fr auto',
                  alignItems: 'center', gap: 12,
                  padding: '6px 12px',
                  borderBottom: i < m.paths.length - 1 ? '0.5px solid var(--border)' : 'none',
                }}>
                  <span className="eyebrow" style={{
                    whiteSpace: 'nowrap',
                  }}>{p.os}</span>
                  <span style={{
                    fontFamily: 'var(--font-mono)', fontSize: 11.5,
                    color: 'var(--text)',
                    overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                  }}>{p.path}</span>
                  <CopyButton text={p.path} />
                </div>
              ))}
            </div>
          </details>
        </ConfigSection>

        {/* 02 — Verify */}
        <ConfigSection
          step={2}
          title="Verify"
          subtitle="Run a test install — the request will appear in monitoring within ~2s"
        >
          <ShellSnippet body={fill(m.verify.body)} url={url} host={host} />
        </ConfigSection>

        {/* 03 — Step-by-step (collapsible) */}
        <ConfigSection
          step={3}
          title="Step-by-step"
          subtitle="Walk through the configuration end-to-end"
        >
          <details>
            <summary style={{
              padding: '8px 12px',
              fontSize: 11, color: 'var(--text-muted)',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 6,
              cursor: 'pointer',
              listStyle: 'none',
              display: 'flex', alignItems: 'center', gap: 6,
            }}>
              <span style={{ color: 'var(--text-subtle)' }}>▸</span>
              Show {m.tutorial.length} steps
            </summary>
            <ol style={{
              margin: '8px 0 0 0', paddingLeft: 0, listStyle: 'none',
              display: 'flex', flexDirection: 'column', gap: 6,
            }}>
              {m.tutorial.map((step, i) => (
                <li key={i} style={{
                  display: 'grid',
                  gridTemplateColumns: '24px 1fr',
                  alignItems: 'flex-start', gap: 10,
                  padding: '8px 12px',
                  background: 'var(--bg-soft)',
                  border: '0.5px solid var(--border)',
                  borderRadius: 6,
                }}>
                  <span style={{
                    width: 18, height: 18, borderRadius: 4,
                    background: 'var(--brand-soft)',
                    color: 'var(--brand)',
                    fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 500,
                    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                    marginTop: 1, flexShrink: 0,
                  }}>{i + 1}</span>
                  <span style={{ fontSize: 12, lineHeight: 1.6, color: 'var(--text)' }}>{fill(step)}</span>
                </li>
              ))}
            </ol>
          </details>
        </ConfigSection>
      </div>

      <LiveDetector endpoint={endpoint} manager={m.id} />
    </div>
  );
}

// ────────────────────────────────────────────────────────────
// Live request detector — listens for requests passing the proxy
// ────────────────────────────────────────────────────────────
function LiveDetector({ endpoint, manager }) {
  const [hits, setHits] = useState([]); // {id, path, ms, t}
  const [tick, setTick] = useState(0);

  // Simulated request stream — in real impl this would be SSE/websocket
  useEffect(() => {
    const samples = {
      pip: ['simple/requests/', 'simple/numpy/', 'simple/fastapi/'],
      poetry: ['simple/pydantic/', 'simple/httpx/'],
      uv: ['simple/polars/', 'simple/ruff/'],
      npm: ['react/-/react-18.3.1.tgz', 'lodash/-/lodash-4.17.21.tgz'],
      pnpm: ['next/-/next-14.2.0.tgz'],
      yarn: ['typescript/-/typescript-5.4.5.tgz'],
      cargo: ['api/v1/crates/serde/1.0.197/download'],
      go: ['github.com/spf13/cobra/@v/v1.8.0.zip'],
      docker: ['v2/library/alpine/manifests/3.19'],
    };
    const paths = samples[manager] || ['simple/requests/'];
    let n = 0;
    const id = setInterval(() => {
      const path = paths[n % paths.length];
      n++;
      const ms = 30 + Math.floor(Math.random() * 90);
      const hit = { id: Date.now() + Math.random(), path, ms, t: Date.now() };
      setHits(h => [hit, ...h].slice(0, 3));
    }, 4500 + Math.random() * 3000);
    return () => clearInterval(id);
  }, [manager]);

  // re-render every 1s for the relative timestamps
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 1000);
    return () => clearInterval(id);
  }, []);

  const latest = hits[0];
  const fresh = latest && (Date.now() - latest.t) < 4000;

  return (
    <div style={{
      borderTop: '0.5px solid var(--border)',
      padding: '8px 14px',
      display: 'flex', alignItems: 'center', gap: 10,
      background: 'var(--bg-card)', flexShrink: 0,
      minHeight: 36,
    }}>
      <span className="dot-live" style={{
        width: 6, height: 6, borderRadius: '50%',
        background: hits.length ? 'var(--ok)' : 'var(--text-subtle)',
        color: hits.length ? 'var(--ok)' : 'var(--text-subtle)',
        flexShrink: 0,
      }}/>
      {hits.length === 0 ? (
        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
          Listening for requests on <span className="mono" style={{ color: 'var(--text-subtle)' }}>{endpoint}</span>… run the verify command above.
        </span>
      ) : (
        <>
          <span style={{
            fontSize: 11, color: fresh ? 'var(--ok-text)' : 'var(--text-muted)',
            fontWeight: fresh ? 500 : 400,
            transition: 'color 300ms',
            whiteSpace: 'nowrap',
          }}>
            {hits.length} request{hits.length > 1 ? 's' : ''} detected
          </span>
          <span style={{ color: 'var(--border-strong)' }}>·</span>
          <span className="mono" style={{
            fontSize: 11, color: 'var(--text)',
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
            flex: 1, minWidth: 0,
          }}>{latest.path}</span>
          <span className="mono" style={{
            fontSize: 10, color: 'var(--text-subtle)',
            padding: '2px 6px',
            background: 'var(--bg-soft)',
            border: '0.5px solid var(--border)',
            borderRadius: 4,
            flexShrink: 0,
          }}>{latest.ms}ms</span>
          <span style={{
            fontSize: 10, color: 'var(--text-subtle)',
            flexShrink: 0, whiteSpace: 'nowrap',
          }}>{relTime(latest.t)}</span>
        </>
      )}
    </div>
  );
}

function relTime(t) {
  const s = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (s < 1) return 'now';
  if (s < 60) return `${s}s ago`;
  return `${Math.floor(s / 60)}m ago`;
}

function QuickStartPage({ endpoint, language, setLanguage }) {
  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <h1 className="grad-text" style={{ margin: 0, fontSize: 44, fontWeight: 700, letterSpacing: '-0.04em', lineHeight: 1.02 }}>Quick start</h1>
          <p style={{ margin: '14px 0 0 0', fontSize: 17, lineHeight: 1.45, color: 'var(--text)', maxWidth: 580, fontWeight: 400, letterSpacing: '-0.005em' }}>
            Pick a language, choose a package manager, copy the snippet — <span style={{ color: 'var(--text-soft)' }}>or grab the AI prompt for your assistant.</span>
          </p>
        </div>
        <EndpointInline endpoint={endpoint} />
      </div>

      <div style={{
        display: 'grid',
        gridTemplateColumns: '240px 1fr',
        gap: 16,
        height: 720,
      }}>
        <LanguageRail selected={language} onSelect={setLanguage} />
        {language === 'all'
          ? <AllInOnePane endpoint={endpoint} />
          : <ConfigurePane language={language} endpoint={endpoint} />}
      </div>
    </div>
  );
}

window.QuickStartPage = QuickStartPage;
