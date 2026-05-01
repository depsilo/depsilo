/* ============================================================
   Depsilo — App shell + state
   ============================================================ */

const ENDPOINT_DEFAULT = 'http://localhost:8080';

const TWEAKS_DEFAULT = /*EDITMODE-BEGIN*/{
  "accent": "#7A59C3",
  "showStatusBanner": true,
  "daemonStatus": "degraded",
  "endpoint": "http://localhost:8080"
}/*EDITMODE-END*/;

function App() {
  const [t, setTweak] = useTweaks(TWEAKS_DEFAULT);
  const [tab, setTab] = useState('quickstart');
  const [theme, setTheme] = useState('light');
  const [lang, setLang] = useState('en');
  const [timeRange, setTimeRange] = useState('1h');
  const [language, setLanguage] = useState('all');

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
  }, [theme]);

  useEffect(() => {
    if (!t.accent) return;
    document.documentElement.style.setProperty('--brand', t.accent);
    const hex = t.accent.replace('#', '');
    if (hex.length === 6) {
      const r = parseInt(hex.slice(0, 2), 16);
      const g = parseInt(hex.slice(2, 4), 16);
      const b = parseInt(hex.slice(4, 6), 16);
      document.documentElement.style.setProperty('--brand-soft', `rgba(${r},${g},${b},0.08)`);
      document.documentElement.style.setProperty('--brand-border', `rgba(${r},${g},${b},0.32)`);
    }
  }, [t.accent]);

  const daemonStatus = t.showStatusBanner ? t.daemonStatus : 'connected';

  return (
    <div data-screen-label="Depsilo Console" style={{ minHeight: '100vh', position: 'relative' }}>
      <div className="page-wash" />
      <TopNav
        tab={tab} setTab={setTab}
        theme={theme} setTheme={setTheme}
        lang={lang} setLang={setLang}
        daemonStatus={daemonStatus}
      />
      <main style={{ maxWidth: 1240, margin: '0 auto', padding: '32px 28px 64px', position: 'relative', zIndex: 1 }}>
        {tab === 'monitor' && <MonitorPage timeRange={timeRange} setTimeRange={setTimeRange} daemonStatus={daemonStatus} />}
        {tab === 'quickstart' && <QuickStartPage endpoint={t.endpoint || ENDPOINT_DEFAULT} language={language} setLanguage={setLanguage} />}
      </main>

      <TweaksPanel title="Tweaks">
        <TweakSection label="Brand" />
        <TweakColor label="Accent" value={t.accent} onChange={v => setTweak('accent', v)} />

        <TweakSection label="Daemon state" />
        <TweakRadio
          label="Status"
          value={t.daemonStatus}
          options={['connected', 'degraded', 'offline']}
          onChange={v => setTweak('daemonStatus', v)}
        />
        <TweakToggle
          label="Show status banner"
          value={t.showStatusBanner}
          onChange={v => setTweak('showStatusBanner', v)}
        />

        <TweakSection label="Proxy" />
        <TweakText
          label="Endpoint"
          value={t.endpoint}
          onChange={v => setTweak('endpoint', v)}
        />
      </TweaksPanel>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App />);
