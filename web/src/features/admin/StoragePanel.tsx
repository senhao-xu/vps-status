import { useState } from 'react'

import { apiMutation } from '../../api/client'
import type { StorageStats } from '../../api/contracts'
import { formatBytes, formatTime } from '../../format'
import { useI18n } from '../../i18n'
import { SettingsFeedback } from './SettingsFeedback'
import { useAdminAction } from './useAdminAction'

export function StoragePanel({ storage, refresh, onUnauthorized }: { storage: StorageStats; refresh: () => void; onUnauthorized: () => void }) {
  const { language, t } = useI18n()
  const [clearConfirmation, setClearConfirmation] = useState('')
  const [resetConfirmation, setResetConfirmation] = useState('')
  const action = useAdminAction(onUnauthorized)
  const perform = (path: string, confirm: string, message: string, pages?: number) => {
    if (confirm === 'clear_history' && clearConfirmation !== confirm) return
    if (confirm === 'reset_all_telemetry' && resetConfirmation !== confirm) return
    void action.run((signal) => apiMutation(`/api/v1/admin/telemetry/${path}`, { signal, body: { confirm, ...(pages === undefined ? {} : { pages }) } }), () => {
      if (confirm === 'clear_history') setClearConfirmation('')
      if (confirm === 'reset_all_telemetry') setResetConfirmation('')
      refresh()
    }, message)
  }
  return <div className="settings-stack" aria-busy={action.pending}>
    <section className="panel settings-intro"><div className="section-heading"><div><h2>{t('storageOverview')}</h2><p className="muted">{t('historySeparate')}</p></div><span className={`status ${storage.state.writable ? 'online' : 'offline'}`}>{storage.state.writable ? t('writable') : storage.state.reason || t('unavailable')}</span></div>
      <dl className="settings-summary"><div><dt>{t('total')}</dt><dd>{formatBytes(storage.totalBytes, language)}</dd></div><div><dt>{t('freeDisk')}</dt><dd>{formatBytes(storage.freeDiskBytes, language)}</dd></div><div><dt>{t('oldestRetained')}</dt><dd>{formatTime(storage.oldestBucket, language)}</dd></div></dl>
      {!storage.state.writable && <p className="notice warning">{t('storageDegradedHint')}</p>}
    </section><SettingsFeedback {...action} />
    <section className="panel"><h2>{t('storageDetails')}</h2><dl className="metric-grid"><div><dt>{t('mainDatabase')}</dt><dd>{formatBytes(storage.mainBytes, language)}</dd></div><div><dt>WAL</dt><dd>{formatBytes(storage.walBytes, language)}</dd></div><div><dt>SHM</dt><dd>{formatBytes(storage.shmBytes, language)}</dd></div><div><dt>{t('freePages')}</dt><dd>{storage.freelistPages?.toLocaleString(language === 'zh' ? 'zh-CN' : 'en') ?? t('unavailable')}</dd></div><div><dt>{t('droppedBuckets')}</dt><dd>{storage.state.dropped.toLocaleString(language === 'zh' ? 'zh-CN' : 'en')}</dd></div><div><dt>{t('writeQueue')}</dt><dd>{storage.state.queueDepth} / {storage.state.queueCapacity}</dd></div></dl></section>
    <section className="panel"><h2>{t('routineMaintenance')}</h2><p className="muted">{t('maintenanceHint')}</p><div className="maintenance-grid">
      <article><h3>{t('checkpointWal')}</h3><p className="muted">{t('checkpointHint')}</p><button disabled={action.pending} onClick={() => perform('checkpoint', 'checkpoint', t('checkpointComplete'))}>{t('checkpointWal')}</button></article>
      <article><h3>{t('reclaimPages')}</h3><p className="muted">{t('reclaimHint')}</p><button disabled={action.pending} onClick={() => perform('incremental-vacuum', 'incremental_vacuum', t('incrementalVacuumComplete'), 1024)}>{t('reclaimPages')}</button></article>
      <article><h3>{t('fullVacuum')}</h3><p className="muted">{t('fullVacuumHint')}</p><button disabled={action.pending} onClick={() => perform('full-vacuum', 'full_vacuum', t('fullVacuumComplete'))}>{t('fullVacuum')}</button></article>
    </div></section>
    <section className="panel settings-danger"><h2>{t('historyCleanup')}</h2><div className="cleanup-grid"><article><h3>{t('clearHistory')}</h3><p className="muted">{t('clearHistoryHint')}</p><label>{t('confirmExact', { value: 'clear_history' })}<input autoComplete="off" spellCheck={false} value={clearConfirmation} disabled={action.pending} onChange={(event) => setClearConfirmation(event.target.value)} /></label><button className="danger" disabled={action.pending || clearConfirmation !== 'clear_history'} onClick={() => perform('clear-history', 'clear_history', t('historyCleared'))}>{t('clearHistory')}</button></article>
      <article><h3>{t('resetTelemetry')}</h3><p className="muted">{t('resetTelemetryHint')}</p><label>{t('confirmExact', { value: 'reset_all_telemetry' })}<input autoComplete="off" spellCheck={false} value={resetConfirmation} disabled={action.pending} onChange={(event) => setResetConfirmation(event.target.value)} /></label><button className="danger" disabled={action.pending || resetConfirmation !== 'reset_all_telemetry'} onClick={() => perform('reset-all', 'reset_all_telemetry', t('telemetryReset'))}>{t('resetTelemetry')}</button></article></div></section>
  </div>
}
