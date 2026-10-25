import { type FormEvent, useEffect, useRef, useState } from 'react'

import { APIError, apiMutation, errorMessage } from '../../api/client'
import type { Node, ProbeTask } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { parseProbeTarget, probeAssignmentWrite, probeSelection, visibleProbeNodes } from './probeConfiguration'
import { probeTarget } from './probePresentation'
import { SettingsDialog } from './SettingsDialog'

export function ProbeTaskDialog({ task, nodes, onClose, onSaved, onUnauthorized }: {
  task: ProbeTask | null; nodes: Node[]; onClose: () => void; onSaved: (edited: boolean) => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const [selection, setSelection] = useState<Map<string, boolean>>(() => new Map())
  const [autoJoin, setAutoJoin] = useState(task?.autoJoin ?? true)
  const [group, setGroup] = useState('')
  const groups = [...new Set(nodes.map((node) => node.group))].sort()
  const chosen = probeSelection(task, nodes, selection)
  const [query, setQuery] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  const visible = visibleProbeNodes(nodes, group === '' ? null : group.slice(1), query)
  const selectedCount = nodes.filter((node) => chosen.has(node.id)).length
  const pick = (ids: string[], checked: boolean) => setSelection((current) => {
    const next = new Map(current)
    for (const id of ids) next.set(id, checked)
    return next
  })
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (request.current) return
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name') ?? '').trim()
    const target = parseProbeTarget(String(form.get('target') ?? ''))
    const interval = Number(form.get('interval'))
    if (!target) { setError(t('probeTargetValidation')); return }
    if (!name || !Number.isInteger(interval) || interval < 5 || interval > 3600) {
      setError(t('probeValidation')); return
    }
    const controller = new AbortController(); request.current = controller
    setPending(true); setError('')
    try {
      await apiMutation(task ? `/api/v1/admin/probes/${encodeURIComponent(task.id)}` : '/api/v1/admin/probes', {
        method: task ? 'PATCH' : 'POST', signal: controller.signal,
        body: { name, ...target, auto_join: autoJoin, interval_seconds: interval, ...probeAssignmentWrite(task, nodes, selection) },
      })
      if (!controller.signal.aborted) onSaved(task !== null)
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setError(caught instanceof APIError && caught.code === 'probe_auto_join_limit' ? t('probeAutoJoinLimit') : errorMessage(caught, t))
    } finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setPending(false) }
  }
  return <SettingsDialog className="probe-dialog" title={t(task ? 'editTcpProbe' : 'addTcpProbe')} pending={pending} onClose={onClose}>
    <p id="probe-dialog-description" className="muted">{t('probeConfigurationDescription')}</p>
    {error && <p id="probe-dialog-error" className="notice error" role="alert">{error}</p>}
    <form className="form-grid" onSubmit={(event) => void submit(event)} aria-describedby={error ? 'probe-dialog-error' : 'probe-dialog-description'}>
      <fieldset className="full probe-fields" disabled={pending}>
        <label className="full">{t('name')}<input data-initial-focus name="name" maxLength={128} defaultValue={task?.name ?? ''} required /></label>
        <label className="full">{t('probeTargetLabel')}<input name="target" defaultValue={task ? probeTarget(task) : ''} placeholder="example.com:443" aria-describedby="probe-target-hint" required /><small id="probe-target-hint" className="muted">{t('probeTargetHint')}</small></label>
        <label className="full">{t('intervalSeconds')}<input name="interval" type="number" min={5} max={3600} step={1} defaultValue={task?.intervalSeconds ?? 60} required /></label>
      </fieldset>
      <fieldset className="full probe-node-picker" disabled={pending}><legend>{t('executionNodes')}</legend>
        {nodes.length > 0 ? <>
          <div className="probe-picker-filters"><label>{t('groups')}<select value={group} onChange={(event) => setGroup(event.target.value)}><option value="">{t('allGroups')}</option>{groups.map((value) => <option value={`g${value}`} key={value}>{value || t('probeUngrouped')}</option>)}</select></label><label>{t('searchNodes')}<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('searchPlaceholder')} /></label></div>
          <div className="probe-picker-heading"><span className="muted">{t('probeSelectedNodes', { selected: selectedCount, total: nodes.length })}</span><div className="button-row"><button type="button" disabled={pending || !visible.length} onClick={() => pick(visible.map((node) => node.id), true)}>{t('selectVisibleNodes')}</button><button type="button" disabled={pending || !visible.length} onClick={() => pick(visible.map((node) => node.id), false)}>{t('probeDeselectShown')}</button></div></div>
          <div className="probe-picker-list">{visible.length === 0 ? <p className="empty">{t('noMatchingNodes')}</p> : visible.map((node) => <label className="check" key={node.id}><input type="checkbox" checked={chosen.has(node.id)} onChange={(event) => pick([node.id], event.target.checked)} /><span>{node.name}<small className="muted">{node.group}{node.group ? ' · ' : ''}{t(node.online ? 'online' : 'offline')}</small></span></label>)}</div>
          {selectedCount === 0 && <p className="muted">{t('probeUnassignedHint')}</p>}
        </> : <p className="muted">{t('probeNoNodesHint')}</p>}
        <label className="check probe-auto-join"><input type="checkbox" checked={autoJoin} onChange={(event) => setAutoJoin(event.target.checked)} /><span>{t('probeAutoJoin')}<small className="muted">{t('probeAutoJoinHint')}</small></span></label>
      </fieldset>
      <div className="full dialog-actions"><button type="button" disabled={pending} onClick={onClose}>{t('cancelChanges')}</button><button className="primary" disabled={pending}>{t(pending ? 'savingProbe' : task ? 'saveProbe' : 'createProbe')}</button></div>
    </form>
  </SettingsDialog>
}

export function DeleteProbeDialog({ task, onClose, onDeleted, onUnauthorized }: {
  task: ProbeTask; onClose: () => void; onDeleted: () => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const request = useRef<AbortController | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => () => request.current?.abort(), [])
  const remove = async () => {
    if (request.current) return
    const controller = new AbortController(); request.current = controller
    setPending(true); setError('')
    try {
      await apiMutation(`/api/v1/admin/probes/${encodeURIComponent(task.id)}`, { method: 'DELETE', signal: controller.signal })
      if (!controller.signal.aborted) onDeleted()
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setError(errorMessage(caught, t))
    } finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setPending(false) }
  }
  return <SettingsDialog className="probe-dialog" title={t('deleteNamed', { name: task.name })} pending={pending} onClose={onClose}>
    <p>{t('confirmDeleteProbe', { name: task.name })}</p>
    {error && <p className="notice error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" data-initial-focus disabled={pending} onClick={onClose}>{t('cancelChanges')}</button><button type="button" className="danger" disabled={pending} onClick={() => void remove()}>{t(pending ? 'deletingProbe' : 'delete')}</button></div>
  </SettingsDialog>
}
