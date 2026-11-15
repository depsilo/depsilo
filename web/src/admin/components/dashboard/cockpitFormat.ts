import { useTranslation } from 'react-i18next'


/** The locale used for numbers and dates on the Overview. */
export function useNumberLocale(): string {
  const { i18n } = useTranslation()
  return i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en'
}

export { ecosystemLabel } from '@/admin/operatorEcosystems'
