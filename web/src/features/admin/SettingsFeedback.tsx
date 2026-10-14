import { useI18n } from '../../i18n'

export function SettingsFeedback({ error, notice, pending }: { error: string; notice: string; pending: boolean }) {
  const { t } = useI18n()
  return <div aria-busy={pending}>{pending && <p className="muted" role="status">{t('savingSettings')}</p>}{error && <p className="notice error" role="alert">{error}</p>}{notice && <p className="notice success" role="status">{notice}</p>}</div>
}
