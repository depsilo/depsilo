import { Check, ChevronDown, Package } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Badge from '@/components/app/badge'
import Input from '@/components/app/input'
import CodeBlock from '@/components/app/code-block'
import { cn } from '@/lib/utils'

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
      className={cn(
        'flex min-h-12 min-w-0 cursor-pointer flex-col items-start justify-center rounded-sm border px-3 py-2 text-left transition-[background,border-color,color,transform] duration-150 hover:bg-accent active:scale-[0.98]',
        active ? 'border-border bg-accent text-primary' : 'border-transparent bg-transparent text-foreground',
      )}
    >
      <span className="flex w-full min-w-0 items-center justify-between gap-1.5">
        <span className="truncate text-body font-semibold leading-[1.25]">
          {choice.label}
        </span>
        {active && (
          <span aria-hidden="true" className="inline-flex shrink-0">
            <Check className="icon icon-sm" aria-hidden="true" />
          </span>
        )}
      </span>
      <code className="mt-0.5 font-mono text-micro leading-[1.3] text-muted-foreground">
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
      className="border-y border-border"
    >
      <details className="group [&>summary::-webkit-details-marker]:hidden">
        <summary className="flex min-h-[68px] cursor-pointer list-none items-center gap-3 rounded-sm py-3">
          <span
            aria-hidden="true"
            className="flex size-8 shrink-0 items-center justify-center rounded-sm bg-accent text-primary"
          >
            <Package className="icon icon-sm" aria-hidden="true" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-center gap-2">
              <span
                id={titleId}
                className="text-body font-semibold leading-[1.35] text-foreground"
              >
                {t('quickstart.pytorchIndexTitle')}
              </span>
              <Badge variant="neutral">{t('quickstart.pytorchIndexCache')}</Badge>
            </span>
            <span className="mt-1 block max-w-[72ch] text-label leading-[1.45] text-muted-foreground">
              {t('quickstart.pytorchIndexSummary')}
            </span>
          </span>
          <span className="flex shrink-0 items-center gap-1.5 text-label font-semibold text-primary">
            <span className="hidden sm:inline">
              {t('quickstart.pytorchIndexShowCommand')}
            </span>
            <span className="inline-flex text-muted-foreground transition-transform group-open:rotate-180 group-open:text-foreground">
              <ChevronDown className="icon icon-sm" aria-hidden="true" />
            </span>
          </span>
        </summary>

        <div className="pb-4 sm:pl-11">
          <p className="mb-3 mt-0 max-w-[72ch] text-label leading-[1.5] text-muted-foreground">
            {t('quickstart.pytorchIndexDescription')}
          </p>
          <fieldset className="m-0 min-w-0 border-0 p-0">
            <legend className="mb-2 text-label font-semibold leading-[1.3] text-muted-foreground">
              {t('quickstart.pytorchIndexPlatformLabel')}
            </legend>
            <div
              className="grid max-w-[760px] gap-1 rounded-sm bg-muted p-1"
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
            <p className="mb-0 mt-2 text-label leading-[1.45] text-muted-foreground">
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
                className="mb-3 mt-3 flex min-w-0 flex-col gap-1 rounded-sm bg-muted px-3 py-2"
              >
                <span className="text-meta font-semibold text-muted-foreground">
                  {t('quickstart.pytorchIndexEndpointLabel')}
                </span>
                <code className="break-all font-mono text-meta leading-[1.45] text-primary">
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
