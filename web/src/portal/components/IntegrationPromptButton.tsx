// Button on QuickStart that opens a modal with the brand-neutral
// project-integration prompt. The user copies the prompt into their AI coding
// agent (Claude Code / Cursor / Copilot Chat) and the agent edits Dockerfile /
// CI / build scripts to route installs through this mirror.
//
// Distinct from the "AI prompt" mode inside AllInOnePane, which configures the
// developer's local machine. This one rewrites project source.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import ModalV2 from '@/components/Modal'
import CopyButton from '@/portal/components/CopyButton'

export default function IntegrationPromptButton() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const { data: prompt, isLoading, error } = useQuery<string>({
    queryKey: ['integration-prompt'],
    queryFn: async () => {
      const res = await fetch('/api/v1/integration-prompt')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.text()
    },
    enabled: open,
    staleTime: 5 * 60 * 1000,
  })

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="active:scale-[0.96]"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          padding: '6px 12px',
          background: 'var(--bg-soft)',
          border: '0.5px solid var(--border)',
          borderRadius: 6,
          fontSize: 12,
          color: 'var(--text)',
          cursor: 'pointer',
          flexShrink: 0,
          transition: 'transform 120ms cubic-bezier(0.2, 0, 0, 1), background 120ms ease',
        }}
      >
        <span aria-hidden style={{ fontSize: 13 }}>✦</span>
        {t('quickstart.aiIntegrationButton')}
      </button>

      <ModalV2
        open={open}
        onClose={() => setOpen(false)}
        title={t('quickstart.aiIntegrationTitle')}
        width={720}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.55 }}>
            {t('quickstart.aiIntegrationDesc')}
          </p>

          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '6px 10px',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 6,
            }}
          >
            <span style={{ fontSize: 11, color: 'var(--text-muted)', flex: 1, minWidth: 0 }}>
              {t('quickstart.aiIntegrationHowto')}
            </span>
            {prompt && <CopyButton text={prompt} />}
          </div>

          <pre
            style={{
              margin: 0,
              padding: 14,
              fontFamily: 'var(--font-mono)',
              fontSize: 12,
              lineHeight: 1.55,
              color: 'var(--text)',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 6,
              whiteSpace: 'pre',
              maxHeight: '60vh',
              overflow: 'auto',
              minHeight: 120,
            }}
          >
            {isLoading && <span style={{ color: 'var(--text-muted)' }}>{t('quickstart.loading')}</span>}
            {error && <span style={{ color: 'var(--danger-text)' }}>{t('quickstart.aiIntegrationError')}</span>}
            {prompt && <code style={{ display: 'block' }}>{prompt}</code>}
          </pre>
        </div>
      </ModalV2>
    </>
  )
}
