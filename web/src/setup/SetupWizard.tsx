import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import ButtonV2 from '../components/Button'
import InputV2 from '../components/Input'
import Icon from '../components/Icon'
import EcosystemIcon from '../components/EcosystemIcon'
import { ecosystemDefaults, type UpstreamDefault } from '../setup/defaults'
import { setupApi } from '../lib/api'
import axios from 'axios'

export default function SetupWizard() {
  const { t } = useTranslation()

  const [step, setStep] = useState(1)
  const [port, setPort] = useState(23333)
  const [storagePath, setStoragePath] = useState('./data/cache')
  const [selectedEcosystems, setSelectedEcosystems] = useState<Set<string>>(
    () => new Set(ecosystemDefaults.map((e) => e.key))
  )
  const [upstreams, setUpstreams] = useState<Record<string, UpstreamDefault[]>>(() => {
    const map: Record<string, UpstreamDefault[]> = {}
    for (const eco of ecosystemDefaults) {
      map[eco.key] = eco.upstreams.map((u) => ({ ...u }))
    }
    return map
  })
  const [expandedEcosystem, setExpandedEcosystem] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [restarting, setRestarting] = useState(false)

  const totalSteps = 5

  const toggleEcosystem = useCallback((key: string) => {
    setSelectedEcosystems((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

  const updateUpstream = useCallback(
    (ecoKey: string, index: number, field: keyof UpstreamDefault, value: string | number) => {
      setUpstreams((prev) => {
        const list = [...(prev[ecoKey] || [])]
        list[index] = { ...list[index], [field]: value }
        return { ...prev, [ecoKey]: list }
      })
    },
    []
  )

  const addUpstream = useCallback((ecoKey: string) => {
    setUpstreams((prev) => {
      const list = [...(prev[ecoKey] || [])]
      const priority = list.length > 0 ? Math.max(...list.map((u) => u.priority)) + 1 : 1
      list.push({ name: '', url: '', priority })
      return { ...prev, [ecoKey]: list }
    })
  }, [])

  const removeUpstream = useCallback((ecoKey: string, index: number) => {
    setUpstreams((prev) => {
      const list = [...(prev[ecoKey] || [])]
      list.splice(index, 1)
      return { ...prev, [ecoKey]: list }
    })
  }, [])

  const handleSubmit = async () => {
    setSubmitting(true)
    try {
      const ecosystems: Record<string, { enabled: boolean; upstreams: UpstreamDefault[] }> = {}
      for (const eco of ecosystemDefaults) {
        const enabled = selectedEcosystems.has(eco.key)
        ecosystems[eco.key] = {
          enabled,
          upstreams: enabled ? upstreams[eco.key] || [] : [],
        }
      }

      await setupApi.complete({
        server: { port },
        storage: { path: storagePath },
        ecosystems,
      })

      setSubmitting(false)
      setRestarting(true)

      // Poll /health until server responds, then redirect
      const pollHealth = () => {
        const interval = setInterval(async () => {
          try {
            await axios.get('/health')
            clearInterval(interval)
            window.location.href = '/'
          } catch {
            // Server still restarting, keep polling
          }
        }, 1000)
      }
      pollHealth()
    } catch {
      setSubmitting(false)
    }
  }

  const canNext = () => {
    if (step === 2) return port > 0 && storagePath.trim() !== ''
    if (step === 3) return selectedEcosystems.size > 0
    return true
  }

  // Step 1: Welcome
  const renderWelcome = () => (
    <div className="text-center py-8">
      <div
        className="text-[48px] font-[700] mb-2"
        style={{ color: 'var(--text)' }}
      >
        DepSilo
      </div>
      <p
        className="text-[16px] mb-8 max-w-md mx-auto"
        style={{ color: 'var(--text-soft)' }}
      >
        {t('setup.welcome_description')}
      </p>
      <ButtonV2 onClick={() => setStep(2)}>
        <Icon name="arrow_forward" size="sm" />
        {t('setup.get_started')}
      </ButtonV2>
    </div>
  )

  // Step 2: Basic Settings
  const renderBasicSettings = () => (
    <div className="space-y-6">
      <h2 className="text-[20px] font-[600]" style={{ color: 'var(--text)' }}>
        {t('setup.basic_settings')}
      </h2>
      <InputV2
        label={t('setup.port')}
        type="number"
        value={port}
        min={1}
        max={65535}
        onChange={(e) => setPort(Number(e.target.value))}
      />
      <InputV2
        label={t('setup.storage_path')}
        value={storagePath}
        mono
        onChange={(e) => setStoragePath(e.target.value)}
      />
    </div>
  )

  // Step 3: Select Ecosystems
  const renderSelectEcosystems = () => (
    <div>
      <h2 className="text-[20px] font-[600] mb-4" style={{ color: 'var(--text)' }}>
        {t('setup.select_ecosystems')}
      </h2>
      <p className="text-[14px] mb-4" style={{ color: 'var(--text-soft)' }}>
        {t('setup.select_ecosystems_hint')}
      </p>
      <div className="grid grid-cols-4 gap-3">
        {ecosystemDefaults.map((eco) => {
          const checked = selectedEcosystems.has(eco.key)
          return (
            <button
              key={eco.key}
              type="button"
              className="flex items-center gap-2 rounded-[4px] px-3 py-2.5 text-left cursor-pointer transition-all duration-150"
              style={{
                border: `1px solid ${checked ? 'var(--brand)' : 'var(--border)'}`,
                background: checked ? 'var(--brand-soft)' : 'var(--bg-card)',
                color: 'var(--text)',
              }}
              onClick={() => toggleEcosystem(eco.key)}
            >
              <input
                type="checkbox"
                checked={checked}
                readOnly
                className="accent-[var(--brand)] pointer-events-none"
              />
              <EcosystemIcon type={eco.key as any} size={16} />
              <span className="text-[13px] font-[400] truncate">{eco.label}</span>
            </button>
          )
        })}
      </div>
    </div>
  )

  // Step 4: Configure Upstreams
  const renderConfigureUpstreams = () => {
    const selected = ecosystemDefaults.filter((e) => selectedEcosystems.has(e.key))
    return (
      <div>
        <h2 className="text-[20px] font-[600] mb-4" style={{ color: 'var(--text)' }}>
          {t('setup.configure_upstreams')}
        </h2>
        <div className="space-y-2 max-h-[400px] overflow-y-auto pr-1">
          {selected.map((eco) => {
            const expanded = expandedEcosystem === eco.key
            const ecoUpstreams = upstreams[eco.key] || []
            return (
              <div
                key={eco.key}
                className="rounded-[4px]"
                style={{ border: '1px solid var(--border)' }}
              >
                <button
                  type="button"
                  className="w-full flex items-center gap-2 px-4 py-3 cursor-pointer"
                  style={{ background: 'var(--bg-card)', color: 'var(--text)' }}
                  onClick={() => setExpandedEcosystem(expanded ? null : eco.key)}
                >
                  <Icon name={expanded ? 'expand_more' : 'chevron_right'} size="sm" />
                  <EcosystemIcon type={eco.key as any} size={16} />
                  <span className="text-[14px] font-[500] flex-1 text-left">{eco.label}</span>
                  <span className="text-[12px]" style={{ color: 'var(--text-muted)' }}>
                    {ecoUpstreams.length} {t('setup.upstreams_count')}
                  </span>
                </button>
                {expanded && (
                  <div className="px-4 pb-4 space-y-3" style={{ borderTop: '1px solid var(--border)' }}>
                    {ecoUpstreams.map((upstream, idx) => (
                      <div key={idx} className="flex items-end gap-2 mt-3">
                        <div className="flex-1 min-w-0">
                          <InputV2
                            label={t('setup.upstream_name')}
                            value={upstream.name}
                            onChange={(e) => updateUpstream(eco.key, idx, 'name', e.target.value)}
                          />
                        </div>
                        <div className="flex-[2] min-w-0">
                          <InputV2
                            label={t('setup.upstream_url')}
                            value={upstream.url}
                            mono
                            onChange={(e) => updateUpstream(eco.key, idx, 'url', e.target.value)}
                          />
                        </div>
                        <div className="w-16">
                          <InputV2
                            label={t('setup.priority')}
                            type="number"
                            value={upstream.priority}
                            min={1}
                            onChange={(e) =>
                              updateUpstream(eco.key, idx, 'priority', Number(e.target.value))
                            }
                          />
                        </div>
                        <ButtonV2
                          variant="danger"
                          size="sm"
                          onClick={() => removeUpstream(eco.key, idx)}
                        >
                          <Icon name="delete" size="sm" />
                        </ButtonV2>
                      </div>
                    ))}
                    <ButtonV2 variant="ghost" size="sm" onClick={() => addUpstream(eco.key)}>
                      <Icon name="add" size="sm" />
                      {t('setup.add_upstream')}
                    </ButtonV2>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  // Step 5: Complete
  const renderComplete = () => {
    if (restarting) {
      return (
        <div className="text-center py-12">
          <div
            className="text-[20px] font-[600] mb-4"
            style={{ color: 'var(--text)' }}
          >
            {t('setup.restarting')}
          </div>
          <p className="text-[14px]" style={{ color: 'var(--text-soft)' }}>
            {t('setup.restarting_hint')}
          </p>
        </div>
      )
    }

    const selectedList = ecosystemDefaults.filter((e) => selectedEcosystems.has(e.key))

    return (
      <div>
        <h2 className="text-[20px] font-[600] mb-4" style={{ color: 'var(--text)' }}>
          {t('setup.complete')}
        </h2>
        <div className="space-y-3 mb-6">
          <div className="flex justify-between text-[14px]" style={{ color: 'var(--text-soft)' }}>
            <span>{t('setup.port')}</span>
            <span className="font-mono" style={{ color: 'var(--text)' }}>
              {port}
            </span>
          </div>
          <div className="flex justify-between text-[14px]" style={{ color: 'var(--text-soft)' }}>
            <span>{t('setup.storage_path')}</span>
            <span className="font-mono" style={{ color: 'var(--text)' }}>
              {storagePath}
            </span>
          </div>
          <div
            className="text-[14px] pt-2"
            style={{ color: 'var(--text-soft)', borderTop: '1px solid var(--border)' }}
          >
            <span>{t('setup.enabled_ecosystems')}</span>
          </div>
          <div className="flex flex-wrap gap-2">
            {selectedList.map((eco) => (
              <span
                key={eco.key}
                className="inline-flex items-center gap-1.5 text-[13px] px-2.5 py-1 rounded-full"
                style={{ background: 'var(--brand-soft)', color: 'var(--brand)' }}
              >
                <EcosystemIcon type={eco.key as any} size={14} />
                {eco.label}
              </span>
            ))}
          </div>
        </div>
        <ButtonV2 onClick={handleSubmit} disabled={submitting} className="w-full">
          {submitting ? t('setup.saving') : t('setup.save_and_start')}
        </ButtonV2>
      </div>
    )
  }

  const renderStep = () => {
    switch (step) {
      case 1:
        return renderWelcome()
      case 2:
        return renderBasicSettings()
      case 3:
        return renderSelectEcosystems()
      case 4:
        return renderConfigureUpstreams()
      case 5:
        return renderComplete()
      default:
        return null
    }
  }

  return (
    <div
      className="min-h-screen flex items-center justify-center p-4"
      style={{ background: 'var(--bg-page)' }}
    >
      <div className="w-full max-w-[720px]">
        {/* Progress indicator */}
        {step > 1 && !restarting && (
          <div className="flex items-center justify-center gap-2 mb-6">
            {Array.from({ length: totalSteps }, (_, i) => i + 1).map((s) => (
              <div key={s} className="flex items-center gap-2">
                <div
                  className="w-7 h-7 rounded-full flex items-center justify-center text-[12px] font-[500]"
                  style={{
                    background: s <= step ? 'var(--brand)' : 'var(--bg-soft)',
                    color: s <= step ? 'white' : 'var(--text-muted)',
                    border: s <= step ? 'none' : '1px solid var(--border)',
                  }}
                >
                  {s}
                </div>
                {s < totalSteps && (
                  <div
                    className="w-8 h-[2px]"
                    style={{
                      background: s < step ? 'var(--brand)' : 'var(--border)',
                    }}
                  />
                )}
              </div>
            ))}
            <span className="ml-3 text-[13px]" style={{ color: 'var(--text-muted)' }}>
              {t('setup.step_of', { current: step, total: totalSteps })}
            </span>
          </div>
        )}

        <div
          className="p-5"
          style={{
            background: 'var(--bg-card)',
            border: '0.5px solid var(--border)',
            borderRadius: 'var(--r-card)',
            boxShadow: 'var(--shadow-card)',
          }}
        >
          {renderStep()}

          {/* Navigation buttons */}
          {step > 1 && step < 5 && (
            <div className="flex justify-between mt-6 pt-4" style={{ borderTop: '1px solid var(--border)' }}>
              <ButtonV2 variant="ghost" onClick={() => setStep(step - 1)}>
                <Icon name="arrow_back" size="sm" />
                {t('setup.prev')}
              </ButtonV2>
              <ButtonV2 onClick={() => setStep(step + 1)} disabled={!canNext()}>
                {t('setup.next')}
                <Icon name="arrow_forward" size="sm" />
              </ButtonV2>
            </div>
          )}
          {step === 5 && !restarting && !submitting && (
            <div className="mt-4 pt-4" style={{ borderTop: '1px solid var(--border)' }}>
              <ButtonV2 variant="ghost" onClick={() => setStep(4)}>
                <Icon name="arrow_back" size="sm" />
                {t('setup.prev')}
              </ButtonV2>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
