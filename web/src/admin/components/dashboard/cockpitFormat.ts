import { useTranslation } from 'react-i18next'

import { operatorEcosystems } from '@/admin/operatorEcosystems'

/** The locale used for numbers and dates on the Overview. */
export function useNumberLocale(): string {
  const { i18n } = useTranslation()
  return i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en'
}

/** An ecosystem's product name, falling back to its id for one this build does not know. */
export function ecosystemLabel(id: string): string {
  return operatorEcosystems.find(ecosystem => ecosystem.id === id)?.label ?? id
}
