import { type FormEvent, useState } from 'react'

import { apiRequest } from '../../api/client'
import { decodePublicPageConfig, type Node, type PublicPageConfig } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { acknowledgePublicDraft, selectNodeIds, type PublicPageDraft } from './publicSelection'
import { SettingsFeedback } from './SettingsFeedback'
import { useAdminAction } from './useAdminAction'

export function PublicConfigPanel({ nodes, config, refresh, onUnauthorized }: { nodes: Node[]; config: PublicPageConfig; refresh: () => void; onUnauthorized: () => void }) {
  const { t } = useI18n()
  const [draft, setDraft] = useState<PublicPageDraft>({ overrides: {}, saved: null })
  const acknowledged = acknowledgePublicDraft(draft, config)
  if (acknowledged !== draft) setDraft(acknowledged)
  const { enabled, title, description, nodeIds } = { ...config, ...acknowledged.overrides }
  const chosen = new Set(nodeIds)
  const edit = (overrides: Partial<PublicPageConfig>) => setDraft((current) => ({ overrides: { ...current.overrides, ...overrides }, saved: null }))
  const [query, setQuery] = useState('')
  const [group, setGroup] = useState('*')
  const action = useAdminAction(onUnauthorized)
  const groups = [...new Set(nodes.map((node) => node.group))].sort()
  const visible = nodes.filter((node) => (group === '*' || `group:${node.group}` === group) && `${node.name} ${node.group}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))
  const selected = nodes.filter((node) => chosen.has(node.id))
  const pick = (ids: string[], checked: boolean) => edit({ nodeIds: [...selectNodeIds(chosen, ids, checked)] })
  const save = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const submitted = { enabled, title, description, nodeIds: selected.map((node) => node.id) }
    void action.run((signal) => {
      setDraft({ overrides: submitted, saved: null })
      return apiRequest('/api/v1/admin/public-page', decodePublicPageConfig, { method: 'PUT', signal, body: { enabled, title, description, node_ids: submitted.nodeIds } })
    }, (saved) => { setDraft({ overrides: saved, saved }); refresh() }, t('publicationSaved'))
  }
  return <div className="settings-stack"><section className="panel settings-intro"><div className="section-heading"><div><h2>{t('publicationSettings')}</h2><p className="muted">{t('publicationHint')}</p></div><a href="/" target="_blank" rel="noreferrer">{t('openPublicStatus')}</a></div></section>
    <SettingsFeedback {...action} /><form className="settings-stack" onSubmit={save} aria-busy={action.pending}>
      <section className="panel"><h2>{t('siteIdentity')}</h2><fieldset className="settings-fields" disabled={action.pending}><label className="check full"><input type="checkbox" checked={enabled} onChange={(event) => edit({ enabled: event.target.checked })} />{t('enablePublicPage')}</label><label className="full">{t('title')}<input value={title} onChange={(event) => edit({ title: event.target.value })} maxLength={128} required /></label><label className="full">{t('description')}<textarea value={description} onChange={(event) => edit({ description: event.target.value })} maxLength={512} rows={3} /></label></fieldset></section>
      <section className="panel"><div className="section-heading"><div><h2>{t('publishedNodes')}</h2><p className="muted">{t('publicationNodeHint')}</p></div><span className="selection-count">{t('probeSelectedNodes', { selected: selected.length, total: nodes.length })}</span></div><fieldset className="settings-fields" disabled={action.pending}>
        <label>{t('searchNodes')}<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('searchPlaceholder')} /></label><label>{t('group')}<select value={group} onChange={(event) => setGroup(event.target.value)}><option value="*">{t('allGroups')}</option>{groups.map((value) => <option key={value} value={`group:${value}`}>{value || t('ungrouped')}</option>)}</select></label>
        <div className="full probe-picker-heading"><span className="muted">{t('shownNodeCount', { count: visible.length })}</span><div className="button-row"><button type="button" onClick={() => pick(visible.map((node) => node.id), true)}>{t('selectVisibleNodes')}</button><button type="button" onClick={() => pick(visible.map((node) => node.id), false)}>{t('deselectVisibleNodes')}</button><button type="button" onClick={() => edit({ nodeIds: [] })}>{t('clearNodeSelection')}</button></div></div>
        <div className="full publication-picker">{visible.length === 0 ? <p className="empty">{t(nodes.length ? 'noMatchingNodes' : 'noNodes')}</p> : groups.filter((value) => visible.some((node) => node.group === value)).map((value) => {
          const members = visible.filter((node) => node.group === value)
          const allSelected = members.every((node) => chosen.has(node.id))
          return <div className="publication-group" key={value}><div className="section-heading"><strong>{value || t('ungrouped')}</strong><button type="button" onClick={() => pick(members.map((node) => node.id), !allSelected)}>{t(allSelected ? 'deselectGroup' : 'selectGroup')}</button></div>{members.map((node) => <label className="check" key={node.id}><input type="checkbox" checked={chosen.has(node.id)} onChange={(event) => pick([node.id], event.target.checked)} /><span>{node.name}</span></label>)}</div>
        })}</div></fieldset></section>
      <section className="panel publication-preview"><div className="section-heading"><h2>{t('draftPreview')}</h2><span className={`status ${enabled ? 'online' : 'offline'}`}>{t(enabled ? 'enabled' : 'disabled')}</span></div><p className="muted">{t('draftPreviewHint')}</p><div className="publication-preview-surface"><h3>{title || t('publicStatus')}</h3><p>{description}</p>{!enabled && <p className="notice">{t('publicDisabled')}</p>}<div className="publication-preview-nodes">{selected.length === 0 ? <p className="empty">{t('noPublishedNodes')}</p> : selected.map(({ id, name, group: nodeGroup }) => <div key={id}><strong>{name}</strong><span className="muted">{nodeGroup || t('ungrouped')}</span></div>)}</div></div></section>
      <div className="panel settings-save-bar"><button className="primary" disabled={action.pending}>{t(action.pending ? 'savingSettings' : 'savePublication')}</button><span className="muted">{t('publicationSaveHint')}</span></div>
    </form></div>
}
