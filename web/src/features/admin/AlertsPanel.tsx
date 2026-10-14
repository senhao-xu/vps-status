import { Fragment, type FormEvent, useState } from 'react'

import { apiMutation } from '../../api/client'
import type { AlertRule, Node, NotificationChannel } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { SettingsDialog } from './SettingsDialog'
import { SettingsFeedback } from './SettingsFeedback'
import { useAdminAction } from './useAdminAction'

export function AlertsPanel({ nodes, channels, rules, refresh, onUnauthorized }: {
  nodes: Node[]; channels: NotificationChannel[]; rules: AlertRule[]; refresh: () => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const [tab, setTab] = useState<'channels' | 'rules'>('channels')
  const [editor, setEditor] = useState<'channels' | 'rules' | null>(null)
  const [provider, setProvider] = useState('webhook')
  const [scope, setScope] = useState('node')
  const [metric, setMetric] = useState('offline')
  const [query, setQuery] = useState('')
  const action = useAdminAction(onUnauthorized)
  const groups = [...new Set(nodes.map((node) => node.group).filter(Boolean))].sort()
  const scopes = scope === 'node' ? nodes.map((node) => ({ value: node.id, label: node.name })) : groups.map((group) => ({ value: group, label: group }))
  const channelName = (id: string) => channels.find((channel) => channel.id === id)?.name ?? t('removedChannel')
  const scopeName = (rule: AlertRule) => rule.scopeKind === 'group' ? rule.scopeValue : nodes.find((node) => node.id === rule.scopeValue)?.name ?? t('removedNode')
  const metricName = (value: AlertRule['metric']) => value === 'cpu' ? 'CPU %' : t(value === 'memory' ? 'memoryPercent' : value === 'disk' ? 'diskPercent' : value === 'traffic_rate' ? 'trafficRate' : 'offline')
  const update = (path: string, method: string, body: unknown, notice: string) => void action.run((signal) => apiMutation(path, { method, body, signal }), refresh, notice)
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const body = editor === 'channels' ? {
      name: form.get('name'), kind: provider, enabled: true,
      ...(provider === 'webhook' ? { url: form.get('url'), secret: form.get('secret') } : { bot_token: form.get('bot_token'), chat_id: form.get('chat_id') }),
    } : {
      name: form.get('name'), scope_kind: scope, scope_value: form.get('scope_value'), metric,
      threshold: metric === 'offline' ? 0 : Number(form.get('threshold')),
      duration_seconds: Number(form.get('duration')), recovery_hysteresis: metric === 'offline' ? 0 : Number(form.get('hysteresis')),
      enabled: true, reminder_interval_seconds: 0, reminder_max_count: 0, channel_ids: form.getAll('channel_ids').map(String),
    }
    void action.run((signal) => apiMutation(`/api/v1/admin/${editor === 'channels' ? 'notification-channels' : 'alert-rules'}`, { body, signal }), () => { setEditor(null); refresh() }, t(editor === 'channels' ? 'channelCreated' : 'ruleCreated'))
  }
  const search = query.trim().toLocaleLowerCase()
  const shownChannels = channels.filter((channel) => `${channel.name} ${channel.kind}`.toLocaleLowerCase().includes(search))
  const shownRules = rules.filter((rule) => `${rule.name} ${scopeName(rule)} ${metricName(rule.metric)}`.toLocaleLowerCase().includes(search))
  return <div className="settings-stack">
    <section className="panel settings-intro"><div className="section-heading"><div><h2>{t('alertManagement')}</h2><p className="muted">{t('alertManagementHint')}</p></div><button className="primary" disabled={action.pending} onClick={() => { setProvider('webhook'); setScope('node'); setMetric('offline'); setEditor(tab) }}>{t(tab === 'channels' ? 'createChannel' : 'createRule')}</button></div>
      <div className="settings-toolbar"><div className="view-toggle" role="group" aria-label={t('alerts')}><button aria-pressed={tab === 'channels'} onClick={() => { setTab('channels'); setQuery('') }}>{t('notificationChannels')} ({channels.length})</button><button aria-pressed={tab === 'rules'} onClick={() => { setTab('rules'); setQuery('') }}>{t('alertRules')} ({rules.length})</button></div><label>{t('searchAlerts')}<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} /></label></div>
    </section>
    {!editor && <SettingsFeedback {...action} />}
    <section className="panel settings-list" aria-label={t(tab === 'channels' ? 'notificationChannels' : 'alertRules')} aria-busy={action.pending}>
      {tab === 'channels' ? shownChannels.length === 0 ? <p className="empty">{t(channels.length ? 'noMatchingAlerts' : 'noChannels')}</p> : shownChannels.map((channel) => {
        const path = `/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}`
        return <article className="settings-row" key={channel.id}><div className="item-main"><strong>{channel.name}</strong><span>{t(channel.kind)} · {t(channel.configured ? 'configured' : 'incomplete')}</span></div><span className={`status ${channel.enabled ? 'online' : 'offline'}`}>{t(channel.enabled ? 'enabled' : 'disabled')}</span><div className="button-row"><button disabled={action.pending} onClick={() => update(path, 'PATCH', { enabled: !channel.enabled }, t(channel.enabled ? 'channelDisabled' : 'channelEnabled'))}>{t(channel.enabled ? 'disable' : 'enable')}</button><button disabled={action.pending || !channel.enabled} onClick={() => update(`${path}/test`, 'POST', undefined, t('testQueued'))}>{t('test')}</button><button className="danger" disabled={action.pending} onClick={() => { if (window.confirm(t('confirmDeleteChannel', { name: channel.name }))) update(path, 'DELETE', undefined, t('channelDeleted')) }}>{t('delete')}</button></div></article>
      }) : shownRules.length === 0 ? <p className="empty">{t(rules.length ? 'noMatchingAlerts' : 'noRules')}</p> : shownRules.map((rule) => {
        const path = `/api/v1/admin/alert-rules/${encodeURIComponent(rule.id)}`
        return <article className="settings-row" key={rule.id}><div className="item-main"><strong>{rule.name}</strong><span>{metricName(rule.metric)} · {scopeName(rule)}</span><small className="muted">{t('ruleTiming', { seconds: rule.durationSeconds })}{rule.metric !== 'offline' && ` · ${t('threshold')}: ${rule.threshold}`} · {rule.channelIds.map(channelName).join(', ') || t('noChannels')}</small></div><span className={`status ${rule.enabled ? 'online' : 'offline'}`}>{t(rule.enabled ? 'enabled' : 'disabled')}</span><div className="button-row"><button disabled={action.pending} onClick={() => update(path, 'PATCH', { enabled: !rule.enabled }, t(rule.enabled ? 'ruleDisabled' : 'ruleEnabled'))}>{t(rule.enabled ? 'disable' : 'enable')}</button><button className="danger" disabled={action.pending} onClick={() => { if (window.confirm(t('confirmDeleteRule', { name: rule.name }))) update(path, 'DELETE', undefined, t('ruleDeleted')) }}>{t('delete')}</button></div></article>
      })}
    </section>
    {editor && <SettingsDialog title={t(editor === 'channels' ? 'createChannel' : 'createRule')} pending={action.pending} onClose={() => setEditor(null)}>
      <p className="muted">{t(editor === 'channels' ? 'secretsEncrypted' : 'alertRuleHint')}</p><SettingsFeedback {...action} />
      <form onSubmit={submit} className="form-grid"><fieldset className="full settings-fields" disabled={action.pending}>
        <label className="full">{t('name')}<input name="name" required maxLength={128} data-initial-focus /></label>
        {editor === 'channels' ? <><label className="full">{t('kind')}<select value={provider} onChange={(event) => setProvider(event.target.value)}><option value="webhook">{t('webhook')}</option><option value="telegram">{t('telegram')}</option></select></label>{provider === 'webhook' ? <Fragment key="webhook"><label className="full">{t('webhookUrl')}<input name="url" type="url" placeholder="https://…" required /></label><label className="full">{t('webhookSecret')}<input name="secret" type="password" autoComplete="off" required /></label></Fragment> : <Fragment key="telegram"><label className="full">{t('telegramToken')}<input name="bot_token" type="password" autoComplete="off" required /></label><label className="full">{t('telegramChatId')}<input name="chat_id" autoComplete="off" required /></label></Fragment>}</> : <>
          <label>{t('scope')}<select value={scope} onChange={(event) => setScope(event.target.value)}><option value="node">{t('node')}</option><option value="group">{t('group')}</option></select></label><label>{t(scope === 'node' ? 'node' : 'group')}<select key={scope} name="scope_value" defaultValue="" required><option value="" disabled>{t('chooseScope')}</option>{scopes.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label>
          {scopes.length === 0 && <p className="muted full">{t('noAlertScopes')}</p>}
          <label className="full">{t('metric')}<select value={metric} onChange={(event) => setMetric(event.target.value)}>{(['offline', 'cpu', 'memory', 'disk', 'traffic_rate'] as const).map((value) => <option key={value} value={value}>{metricName(value)}</option>)}</select></label>
          {metric !== 'offline' && <><label>{t('threshold')}<input name="threshold" type="number" min={0} defaultValue={90} required /></label><label>{t('recoveryHysteresis')}<input name="hysteresis" type="number" min={0} defaultValue={5} required /></label></>}
          <label className="full">{t('durationSeconds')}<input name="duration" type="number" min={0} max={86400} defaultValue={60} required /></label>
          <fieldset className="full"><legend>{t('channels')}</legend>{channels.length === 0 ? <p className="muted">{t('createChannelFirst')}</p> : channels.map((channel) => <label className="check" key={channel.id}><input type="checkbox" name="channel_ids" value={channel.id} />{channel.name} {!channel.enabled && <small className="muted">{t('disabled')}</small>}</label>)}</fieldset>
        </>}
      </fieldset><div className="full dialog-actions"><button type="button" disabled={action.pending} onClick={() => setEditor(null)}>{t('cancelChanges')}</button><button className="primary" disabled={action.pending || (editor === 'rules' && (!scopes.length || !channels.length))}>{t(action.pending ? 'savingSettings' : editor === 'channels' ? 'createChannel' : 'createRule')}</button></div></form>
    </SettingsDialog>}
  </div>
}
