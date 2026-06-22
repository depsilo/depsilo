// web/src/portal/components/AllInOnePane.tsx
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import CodeBlock from '@/portal/components/CodeBlock'
import PromptCard from '@/portal/components/PromptCard'
import Segmented from '@/components/Segmented'
import { buildAllScript, buildPrompt } from '@/lib/ecosystemData'

interface Props { endpoint: string }

export default function AllInOnePane({ endpoint }: Props) {
  const { t } = useTranslation()
  const [mode, setMode] = useState('script')
  const script = buildAllScript(endpoint)
  const prompt = buildPrompt(endpoint, 'all')
  const MODES = [
    { value: 'script', label: t('quickstart.shellScript') },
    { value: 'prompt', label: t('quickstart.aiPromptMode') },
  ]

  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column' }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          borderBottom: '0.5px solid var(--border)',
          padding: '0 14px',
          height: 44,
          flexShrink: 0,
          gap: 12,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 17,
              fontWeight: 700,
              whiteSpace: 'nowrap',
              letterSpacing: '-0.02em',
              lineHeight: 1.2,
            }}
          >
            {t('quickstart.allInOneTitle')}
          </div>
          <div
            style={{
              fontSize: 12,
              color: 'var(--text-soft)',
              whiteSpace: 'nowrap',
              marginTop: 2,
            }}
          >
            {t('quickstart.allInOneSetupDesc')}
          </div>
        </div>
        <Segmented options={MODES} value={mode} onChange={setMode} />
      </div>
      <div
        style={{
          padding: 16,
          flex: 1,
          overflow: 'auto',
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 14,
        }}
      >
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {mode === 'script' ? t('quickstart.allInOneScriptNote') : t('quickstart.allInOneAINote')}
        </div>
        {mode === 'script' ? (
          <CodeBlock filename="depsilo-setup.sh" code={script} language="sh" />
        ) : (
          <PromptCard prompt={prompt} label={t('quickstart.promptForAnyAI')} />
        )}
      </div>
    </div>
  )
}
