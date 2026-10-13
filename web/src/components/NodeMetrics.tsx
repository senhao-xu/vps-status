import { type ReactNode } from 'react'

import { formatBytes, formatDuration, formatPercent, formatRate, ratio } from '../format'
import { useI18n } from '../i18n'

export function StatusBadge({ online, stale = false }: { online: boolean; stale?: boolean }) {
  const { t } = useI18n()
  return <span className={`status ${online ? 'online' : stale ? 'stale' : 'offline'}`}>{t(online ? 'online' : stale ? 'stale' : 'offline')}</span>
}

export function ResourceMeter({ label, value, detail }: { label: string; value: number | null | undefined; detail?: string }) {
  const { language } = useI18n()
  const percent = value == null ? null : Math.max(0, Math.min(100, value))
  return <div className="resource-meter">
    <div className="resource-label"><span>{label}</span><strong>{formatPercent(value, language)}</strong></div>
    <div className={`resource-track ${percent !== null && percent >= 90 ? 'critical' : percent !== null && percent >= 75 ? 'elevated' : ''}`} aria-hidden="true"><span style={{ width: `${percent ?? 0}%` }} /></div>
    {detail && <small>{detail}</small>}
  </div>
}

export interface NodeMetricValues {
  cpuUsagePercent: number | null; memoryUsedBytes: number | null; memoryTotalBytes: number | null
  diskUsedBytes: number | null; diskTotalBytes: number | null; receiveBytesPerSecond: number | null
  transmitBytesPerSecond: number | null; uptimeSeconds: number | null
}

export function ResourceMeters({ values }: { values: NodeMetricValues | null }) {
  const { language, t } = useI18n()
  return <div className="resource-grid">
    <ResourceMeter label={t('cpu')} value={values?.cpuUsagePercent} />
    <ResourceMeter label={t('memory')} value={ratio(values?.memoryUsedBytes, values?.memoryTotalBytes)} detail={`${formatBytes(values?.memoryUsedBytes, language)} / ${formatBytes(values?.memoryTotalBytes, language)}`} />
    <ResourceMeter label={t('disk')} value={ratio(values?.diskUsedBytes, values?.diskTotalBytes)} detail={`${formatBytes(values?.diskUsedBytes, language)} / ${formatBytes(values?.diskTotalBytes, language)}`} />
  </div>
}

export function NodeCardBody({ name, group, online, stale = false, values, extra }: {
  name: string; group: string; online: boolean; stale?: boolean; values: NodeMetricValues | null; extra?: ReactNode
}) {
  const { language, t } = useI18n()
  const current = online ? values : null
  return <>
    <div className="node-card-heading"><strong className="node-name">{name}</strong><StatusBadge online={online} stale={stale} /></div>
    <span className="node-group">{group || t('ungrouped')}</span>
    <ResourceMeters values={current} />
    <div className="node-network"><span>↓ {formatRate(current?.receiveBytesPerSecond, language)}</span><span>↑ {formatRate(current?.transmitBytesPerSecond, language)}</span></div>
    <div className="node-card-footer"><span>{t('uptime')}</span><strong>{formatDuration(current?.uptimeSeconds, language)}</strong></div>
    {!online && <small className="offline-note">{t('offlineMetrics')}</small>}
    {extra}
  </>
}
