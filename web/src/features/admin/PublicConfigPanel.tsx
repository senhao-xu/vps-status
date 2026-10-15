import { type FormEvent, useState } from 'react'

import { apiRequest } from '../../api/client'
import { decodePublicPageConfig, type PublicPageConfig } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { acknowledgeSiteDraft, type SiteSettings, type SiteSettingsDraft } from './siteSettingsDraft'
import { SettingsFeedback } from './SettingsFeedback'
import { useAdminAction } from './useAdminAction'

export function PublicConfigPanel({ config, refresh, onUnauthorized }: { config: PublicPageConfig; refresh: () => void; onUnauthorized: () => void }) {
  const { t } = useI18n()
  const [draft, setDraft] = useState<SiteSettingsDraft>({ overrides: {}, saved: null })
  const acknowledged = acknowledgeSiteDraft(draft, config)
  if (acknowledged !== draft) setDraft(acknowledged)
  const { enabled, title, description } = { ...config, ...acknowledged.overrides }
  const edit = (overrides: Partial<SiteSettings>) => setDraft((current) => ({ overrides: { ...current.overrides, ...overrides }, saved: null }))
  const action = useAdminAction(onUnauthorized)
  const save = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const submitted = { enabled, title, description }
    void action.run((signal) => {
      setDraft({ overrides: submitted, saved: null })
      return apiRequest('/api/v1/admin/public-page', decodePublicPageConfig, { method: 'PUT', signal, body: submitted })
    }, (saved) => {
      const settings = { enabled: saved.enabled, title: saved.title, description: saved.description }
      setDraft({ overrides: settings, saved: settings })
      refresh()
    }, t('siteSettingsSaved'))
  }
  return <div className="settings-stack"><SettingsFeedback {...action} />
    <form className="settings-stack" onSubmit={save} aria-busy={action.pending}>
      <section className="panel"><div className="section-heading"><h2>{t('siteIdentity')}</h2><a href="/" target="_blank" rel="noreferrer">{t('openPublicStatus')}</a></div>
        <fieldset className="settings-fields" disabled={action.pending}>
          <label className="full">{t('siteName')}<input value={title} onChange={(event) => edit({ title: event.target.value })} maxLength={128} required /></label>
          <label className="full">{t('description')}<textarea value={description} onChange={(event) => edit({ description: event.target.value })} maxLength={512} rows={3} /></label>
        </fieldset>
        <p className="muted">{t('homepageNodesHint')}</p>
      </section>
      <section className="panel"><h2>{t('siteAccess')}</h2><fieldset className="settings-fields" disabled={action.pending}>
        <label className="check full"><input type="checkbox" checked={!enabled} onChange={(event) => edit({ enabled: !event.target.checked })} />{t('accessRequiresLogin')}</label>
        <p className="muted full">{t('siteAccessHint')}</p>
      </fieldset></section>
      <div className="panel settings-save-bar"><button className="primary" disabled={action.pending}>{t(action.pending ? 'savingSettings' : 'saveSiteSettings')}</button></div>
    </form>
  </div>
}
