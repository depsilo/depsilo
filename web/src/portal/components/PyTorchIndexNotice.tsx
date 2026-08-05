import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Badge from '@/components/Badge'
import Icon from '@/components/Icon'
import Input from '@/components/Input'
import CodeBlock from '@/portal/components/CodeBlock'

interface Props {
  endpoint: string
  path: string
  client: 'pip' | 'uv'
}

interface ChannelChoice {
  channel: string
  label: string
}

const validChannel = /^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$/

// Convenience choices mirror the current stable wheel channels published at
// https://pytorch.org/get-started/previous-versions/. The custom choice keeps
// Depsilo compatible with older and newly added official channels.
const channelChoices: ChannelChoice[] = [
  { channel: 'cpu', label: 'CPU' },
  { channel: 'cu126', label: 'CUDA 12.6' },
  { channel: 'cu130', label: 'CUDA 13.0' },
  { channel: 'cu132', label: 'CUDA 13.2' },
  { channel: 'rocm7.2', label: 'ROCm 7.2' },
]

function ChannelButton({
  choice,
  active,
  onSelect,
}: {
  choice: ChannelChoice
  active: boolean
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      aria-label={choice.label}
      aria-pressed={active}
      onClick={onSelect}
      className="stripe-focus-ring flex min-h-12 min-w-0 cursor-pointer flex-col items-start justify-center rounded-[6px] border px-3 py-2 text-left transition-[background,border-color,color,transform] duration-150 hover:bg-[var(--bg-hover)] active:scale-[0.98]"
      style={{
        background: active ? 'var(--brand-soft)' : 'transparent',
        borderColor: active ? 'var(--brand-border)' : 'transparent',
        color: active ? 'var(--brand-text)' : 'var(--text)',
      }}
    >
      <span className="flex w-full min-w-0 items-center justify-between gap-1.5">
        <span className="truncate text-[13px] font-[650] leading-[1.25]">
          {choice.label}
        </span>
        {active && (
          <span aria-hidden="true" className="inline-flex shrink-0">
            <Icon name="check" size="sm" />
          </span>
        )}
      </span>
      <code className="mt-0.5 font-[var(--font-mono)] text-[10px] leading-[1.3] text-[var(--text-subtle)]">
        {choice.channel}
      </code>
    </button>
  )
}

export default function PyTorchIndexNotice({ endpoint, path, client }: Props) {
  const { t } = useTranslation()
  const titleId = useId()
  const [selectedChannel, setSelectedChannel] = useState('cpu')
  const [customChannel, setCustomChannel] = useState('')
  const [customMode, setCustomMode] = useState(false)
  const serviceURL = endpoint.replace(/\/+$/, '')
  const route = path.replace(/^\/+|\/+$/g, '')
  const channel = customMode ? customChannel : selectedChannel
  const normalizedChannel = channel.trim().toLowerCase()
  const channelHasValue = normalizedChannel.length > 0
  const channelIsValid =
    channelHasValue &&
    channel.trim() === normalizedChannel &&
    validChannel.test(normalizedChannel)
  const indexURL = `${serviceURL}/${route}/${normalizedChannel}/simple/`
  const plainHTTP = /^http:\/\//i.test(serviceURL)
  const host = serviceURL.replace(/^https?:\/\//i, '')
  const executable = client === 'uv' ? 'uv pip' : 'pip'
  const command = [
    `${executable} install torch torchvision torchaudio \\`,
    `  --index-url ${indexURL}${plainHTTP ? ' \\' : ''}`,
    ...(plainHTTP ? [`  --trusted-host ${host}`] : []),
  ].join('\n')
  const commandName = t('quickstart.pytorchIndexCommand', {
    client: executable,
    channel: normalizedChannel,
  })

  return (
    <aside
      data-pytorch-index
      aria-labelledby={titleId}
      className="border-y border-[var(--border)]"
    >
      <details className="config-disclosure">
        <summary className="stripe-focus-ring flex min-h-[68px] cursor-pointer list-none items-center gap-3 rounded-[6px] py-3">
          <span
            aria-hidden="true"
            className="flex size-8 shrink-0 items-center justify-center rounded-[7px] bg-[var(--brand-soft)] text-[var(--brand-text)]"
          >
            <Icon name="inventory_2" size="sm" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-center gap-2">
              <span
                id={titleId}
                className="text-[13px] font-[650] leading-[1.35] text-[var(--text)]"
              >
                {t('quickstart.pytorchIndexTitle')}
              </span>
              <Badge variant="neutral">{t('quickstart.pytorchIndexCache')}</Badge>
            </span>
            <span className="mt-1 block max-w-[72ch] text-[12px] leading-[1.45] text-[var(--text-muted)]">
              {t('quickstart.pytorchIndexSummary')}
            </span>
          </span>
          <span className="flex shrink-0 items-center gap-1.5 text-[12px] font-[600] text-[var(--brand-text)]">
            <span className="hidden sm:inline">
              {t('quickstart.pytorchIndexShowCommand')}
            </span>
            <span className="disclosure-chevron inline-flex">
              <Icon name="expand_more" size="sm" />
            </span>
          </span>
        </summary>

        <div className="pb-4 sm:pl-11">
          <p className="mb-3 mt-0 max-w-[72ch] text-[12px] leading-[1.5] text-[var(--text-muted)]">
            {t('quickstart.pytorchIndexDescription')}
          </p>
          <fieldset className="m-0 min-w-0 border-0 p-0">
            <legend className="mb-2 text-[12px] font-[650] leading-[1.3] text-[var(--text-muted)]">
              {t('quickstart.pytorchIndexPlatformLabel')}
            </legend>
            <div
              className="grid max-w-[760px] gap-1 rounded-[8px] bg-[var(--bg-soft)] p-1"
              style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(106px, 1fr))' }}
            >
              {channelChoices.map(choice => (
                <ChannelButton
                  key={choice.channel}
                  choice={choice}
                  active={!customMode && selectedChannel === choice.channel}
                  onSelect={() => {
                    setSelectedChannel(choice.channel)
                    setCustomMode(false)
                  }}
                />
              ))}
              <ChannelButton
                choice={{
                  channel: '{channel}',
                  label: t('quickstart.pytorchIndexOtherChannel'),
                }}
                active={customMode}
                onSelect={() => setCustomMode(true)}
              />
            </div>
            <p className="mb-0 mt-2 text-[12px] leading-[1.45] text-[var(--text-muted)]">
              {t('quickstart.pytorchIndexPlatformHint')}
            </p>
          </fieldset>

          {customMode && (
            <div className="mb-3 mt-3 max-w-[420px]">
              <Input
                label={t('quickstart.pytorchIndexCustomChannelLabel')}
                value={customChannel}
                onChange={event => setCustomChannel(event.target.value)}
                maxLength={64}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                mono
                hint={
                  !channelHasValue || channelIsValid
                    ? t('quickstart.pytorchIndexCustomChannelHint')
                    : undefined
                }
                error={
                  channelHasValue && !channelIsValid
                    ? t('quickstart.pytorchIndexChannelError')
                    : undefined
                }
              />
            </div>
          )}
          {channelIsValid && (
            <>
              <div
                aria-live="polite"
                aria-atomic="true"
                className="mb-3 mt-3 flex min-w-0 flex-col gap-1 rounded-[6px] bg-[var(--bg-soft)] px-3 py-2"
              >
                <span className="text-[11px] font-[620] text-[var(--text-subtle)]">
                  {t('quickstart.pytorchIndexEndpointLabel')}
                </span>
                <code className="break-all font-[var(--font-mono)] text-[11px] leading-[1.45] text-[var(--brand-text)]">
                  {indexURL}
                </code>
              </div>
              <CodeBlock
                filename={`${executable} · ${normalizedChannel}`}
                code={command}
                language="sh"
                copyName={commandName}
              />
            </>
          )}
        </div>
      </details>
    </aside>
  )
}
