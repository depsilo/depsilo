import { Button } from '@/components/ui/button'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { adminApi } from '@/lib/api'
import { buildVersionLabel, providedBuildValue } from '@/lib/buildVersion'

export default function SidebarVersion({ version }: { version?: string }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['admin', 'capabilities', 'summary'],
    queryFn: ({ signal }) => adminApi.getCapabilitySummary({ signal }),
    staleTime: 60_000,
    retry: false,
  })
  const build = query.data?.data ?? { version }
  const pending = query.isPending && !version
  const label = pending ? '…' : buildVersionLabel(build)
  const fields = [
    [t('nav.version'), build.version],
    [t('nav.commit'), 'commit' in build ? build.commit : undefined],
    [t('nav.buildDate'), 'build_date' in build ? build.build_date : undefined],
  ]

  return (
    <Tooltip>
      <TooltipTrigger render={<Button variant="ghost"
        type="button"
        data-admin-version
        aria-label={`${t('nav.versionDetails')}: ${label}`}
        aria-busy={pending || undefined}
        className="ml-auto min-h-[40px] min-w-[40px] max-w-[88px] whitespace-normal px-0"
      >
        <span
          className="block min-w-0 break-all rounded-sm border px-1.5 py-1 font-mono text-[11px] leading-tight tabular-nums"
          style={{ background: 'var(--bg-hover)', color: 'var(--text-soft)', borderColor: 'var(--border)' }}
        >
          {label}
        </span>
      </Button>} />
      <TooltipContent>{(
        <span className="block max-w-[260px] space-y-1 break-all leading-relaxed">
          {pending ? t('loading') : fields.map(([name, value]) => (
            <span key={name} className="block">
              <span className="font-semibold">{name}: </span>
              {providedBuildValue(value) ?? t('nav.versionUnavailable')}
            </span>
          ))}
        </span>
      )}</TooltipContent>
    </Tooltip>
  )
}
