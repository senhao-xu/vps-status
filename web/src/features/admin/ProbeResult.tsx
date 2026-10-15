import type { ProbeTask } from '../../api/contracts'
import { formatTime } from '../../format'
import { useI18n } from '../../i18n'
import { latestProbeResult } from './probePresentation'

export function ProbeResult({ task, nodeId }: { task: ProbeTask; nodeId: string }) {
  const { language, t } = useI18n()
  const result = latestProbeResult(task, nodeId)
  return <span className="probe-result">{!result ? <span className="status stale">{t('probeWaiting')}</span> : <><span className={`status ${result.success ? 'online' : 'offline'}`}>{result.success ? t('probeSuccess') : t('probeFailure')}{result.success && ` · ${result.latencyMillis.toFixed(1)} ms`}</span>{!result.success && result.error && <small className="muted">{result.error}</small>}<time className="muted" dateTime={result.receivedAt}>{formatTime(result.receivedAt, language)}</time></>}</span>
}
