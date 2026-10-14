import { type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'

import { APIError, apiMutation, errorMessage } from '../../api/client'
import type { Node, ProbeTask } from '../../api/contracts'
import { useI18n } from '../../i18n'

function ProbeDialog({ title, children, pending, onClose }: {
  title: string; children: ReactNode; pending: boolean; onClose: () => void
}) {
  const { t } = useI18n()
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const element = dialog.current
    const previous = document.activeElement
    element?.showModal()
    element?.querySelector<HTMLElement>('[data-initial-focus]')?.focus()
    return () => { element?.close(); if (previous instanceof HTMLElement && previous.isConnected) previous.focus() }
  }, [])
  return <dialog ref={dialog} className="enrollment-dialog probe-dialog" aria-labelledby="probe-dialog-title" aria-busy={pending} onCancel={(event) => { event.preventDefault(); if (!pending) onClose() }}>
    <header className="dialog-heading"><h2 id="probe-dialog-title">{title}</h2><button type="button" disabled={pending} aria-label={t('close')} onClick={onClose}>×</button></header>
    {children}
  </dialog>
}

export function ProbeTaskDialog({ task, nodes, onClose, onSaved, onUnauthorized }: {
  task: ProbeTask | null; nodes: Node[]; onClose: () => void; onSaved: (edited: boolean) => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const [chosen, setChosen] = useState(() => new Set(task?.nodeIds ?? []))
  const [query, setQuery] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  const visible = nodes.filter((node) => `${node.name} ${node.group}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))
  const selectedCount = nodes.filter((node) => chosen.has(node.id)).length
  const pick = (ids: string[], checked: boolean) => setChosen((current) => {
    const next = new Set(current)
    for (const id of ids) { if (checked) next.add(id); else next.delete(id) }
    return next
  })
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (request.current) return
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name') ?? '').trim(), host = String(form.get('host') ?? '').trim()
    const port = Number(form.get('port')), interval = Number(form.get('interval'))
    if (!name || !host || !Number.isInteger(port) || port < 1 || port > 65535 || !Number.isInteger(interval) || interval < 5 || interval > 3600) {
      setError(t('probeValidation')); return
    }
    const controller = new AbortController(); request.current = controller
    setPending(true); setError('')
    try {
      await apiMutation(task ? `/api/v1/admin/probes/${encodeURIComponent(task.id)}` : '/api/v1/admin/probes', {
        method: task ? 'PATCH' : 'POST', signal: controller.signal,
        body: { name, host, port, interval_seconds: interval, node_ids: nodes.filter((node) => chosen.has(node.id)).map((node) => node.id) },
      })
      if (!controller.signal.aborted) onSaved(task !== null)
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setError(errorMessage(caught, t))
    } finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setPending(false) }
  }
  return <ProbeDialog title={t(task ? 'editTcpProbe' : 'addTcpProbe')} pending={pending} onClose={onClose}>
    <p id="probe-dialog-description" className="muted">{t('tcpProbeDescription')}</p>
    {error && <p id="probe-dialog-error" className="notice error" role="alert">{error}</p>}
    <form className="form-grid" onSubmit={(event) => void submit(event)} aria-describedby={error ? 'probe-dialog-error' : 'probe-dialog-description'}>
      <fieldset className="full probe-fields" disabled={pending}>
        <label className="full">{t('name')}<input data-initial-focus name="name" maxLength={128} defaultValue={task?.name ?? ''} required /></label>
        <label className="full">{t('host')}<input name="host" defaultValue={task?.host ?? ''} placeholder="example.com" required /></label>
        <label>{t('port')}<input name="port" type="number" min={1} max={65535} step={1} defaultValue={task?.port ?? 443} required /></label>
        <label>{t('intervalSeconds')}<input name="interval" type="number" min={5} max={3600} step={1} defaultValue={task?.intervalSeconds ?? 30} required /></label>
      </fieldset>
      <fieldset className="full probe-node-picker" disabled={pending}><legend>{t('executionNodes')}</legend>
        <div className="probe-picker-heading"><span className="muted">{t('probeSelectedNodes', { selected: selectedCount, total: nodes.length })}</span><div className="button-row"><button type="button" onClick={() => pick(visible.map((node) => node.id), true)}>{t('selectVisibleNodes')}</button><button type="button" onClick={() => setChosen(new Set())}>{t('clearNodeSelection')}</button></div></div>
        <label>{t('searchNodes')}<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('searchPlaceholder')} /></label>
        <div className="probe-picker-list">{visible.length === 0 ? <p className="empty">{t(nodes.length ? 'noMatchingNodes' : 'noNodes')}</p> : visible.map((node) => <label className="check" key={node.id}><input type="checkbox" checked={chosen.has(node.id)} onChange={(event) => pick([node.id], event.target.checked)} /><span>{node.name}<small className="muted">{node.group}{node.group ? ' · ' : ''}{t(node.online ? 'online' : 'offline')}</small></span></label>)}</div>
        {selectedCount === 0 && <p className="muted">{t('probeUnassignedHint')}</p>}
      </fieldset>
      <div className="full dialog-actions"><button type="button" disabled={pending} onClick={onClose}>{t('cancelChanges')}</button><button className="primary" disabled={pending}>{t(pending ? 'savingProbe' : task ? 'saveProbe' : 'createProbe')}</button></div>
    </form>
  </ProbeDialog>
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
  return <ProbeDialog title={t('deleteNamed', { name: task.name })} pending={pending} onClose={onClose}>
    <p>{t('confirmDeleteProbe', { name: task.name })}</p>
    {error && <p className="notice error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" data-initial-focus disabled={pending} onClick={onClose}>{t('cancelChanges')}</button><button type="button" className="danger" disabled={pending} onClick={() => void remove()}>{t(pending ? 'deletingProbe' : 'delete')}</button></div>
  </ProbeDialog>
}
