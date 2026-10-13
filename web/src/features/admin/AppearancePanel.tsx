import { useEffect, useRef, useState } from 'react'

import { APIError, apiRequest, errorMessage } from '../../api/client'
import { builtInThemes, decodeAppearance, type VisualTheme } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { useTheme } from '../../theme'

export function AppearancePanel({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { t } = useI18n()
  const { siteTheme, setSiteTheme, visualOverride, setVisualOverride } = useTheme()
  const [selected, setSelected] = useState<VisualTheme>(siteTheme)
  const [state, setState] = useState<'loading' | 'ready' | 'saving'>('loading')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const translation = useRef(t)
  const readRequest = useRef<AbortController | null>(null)
  const saveRequest = useRef<AbortController | null>(null)
  useEffect(() => { translation.current = t }, [t])
  useEffect(() => {
    const controller = new AbortController()
    readRequest.current = controller
    void apiRequest('/api/v1/admin/appearance', decodeAppearance, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return
      setSiteTheme(value.defaultTheme); setSelected(value.defaultTheme); setState('ready')
    }).catch((caught: unknown) => {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setError(errorMessage(caught, translation.current)); setState('ready')
    })
    return () => { controller.abort(); saveRequest.current?.abort() }
  }, [onUnauthorized, setSiteTheme])
  const save = async () => {
    if (saveRequest.current && !saveRequest.current.signal.aborted) return
    readRequest.current?.abort()
    const controller = new AbortController()
    saveRequest.current = controller
    setState('saving'); setError(''); setNotice('')
    try {
      const value = await apiRequest('/api/v1/admin/appearance', decodeAppearance, { method: 'PUT', signal: controller.signal, body: { default_theme: selected } })
      if (controller.signal.aborted) return
      setSiteTheme(value.defaultTheme); setNotice(translation.current('siteThemeSaved'))
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setError(errorMessage(caught, translation.current))
    } finally {
      if (saveRequest.current === controller) saveRequest.current = null
      if (!controller.signal.aborted) setState('ready')
    }
  }
  return <section className="panel appearance-panel">
    <h2>{t('themeManagement')}</h2><p className="muted">{t('themeManagementDescription')}</p>
    {error && <p className="notice error" role="alert">{error}</p>}{notice && <p className="notice success" role="status">{notice}</p>}
    <div className="theme-previews">{builtInThemes.map((id) => <article className={`theme-choice ${selected === id ? 'chosen' : ''}`} key={id}>
      <div className={`theme-preview preview-${id}`} aria-hidden="true"><div className="preview-top"><i /><i /><i /></div><div className="preview-cards"><div><b /> <span /><span /><span /></div><div><b /><span /><span /><span /></div></div></div>
      <div className="theme-choice-heading"><label className="check"><input type="radio" name="site-theme" value={id} checked={selected === id} disabled={state !== 'ready'} onChange={() => setSelected(id)} />{id === 'emerald' ? 'Emerald' : 'Classic'}</label>{siteTheme === id && <span className="theme-default">{t('siteDefault')}</span>}</div>
      <p className="muted">{t(id === 'emerald' ? 'emeraldDescription' : 'classicDescription')}</p>
      <button type="button" onClick={() => setVisualOverride(id)}>{t('previewTheme', { name: id === 'emerald' ? 'Emerald' : 'Classic' })}</button>
    </article>)}</div>
    {visualOverride && <p className="muted">{t('themeOverrideNotice', { name: visualOverride === 'emerald' ? 'Emerald' : 'Classic' })} <button type="button" onClick={() => setVisualOverride(null)}>{t('followSite')}</button></p>}
    <button className="primary" disabled={state !== 'ready'} onClick={() => void save()}>{state === 'saving' ? t('savingTheme') : t('saveSiteTheme')}</button>
  </section>
}
