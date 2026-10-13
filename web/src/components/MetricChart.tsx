import { formatTime } from '../format'
import { useI18n } from '../i18n'

interface ChartPoint {
  at: string
  value: number | null
}

interface MetricChartProps {
  title: string
  points: ChartPoint[]
  formatValue: (value: number | null) => string
}

export function MetricChart({ title, points, formatValue }: MetricChartProps) {
  const { language, t } = useI18n()
  const valid = points.filter((point): point is ChartPoint & { value: number } => point.value !== null)
  const max = Math.max(1, ...valid.map((point) => point.value))
  const width = 640
  const height = 160
  const line = valid
    .map((point, index) => {
      const sourceIndex = points.indexOf(point)
      const x = points.length <= 1 ? 0 : (sourceIndex / (points.length - 1)) * width
      const y = height - (point.value / max) * (height - 12)
      return `${index === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`
    })
    .join(' ')
  const latest = valid.at(-1)?.value ?? null

  return (
    <section className="chart" aria-labelledby={`chart-${title.replaceAll(' ', '-').toLowerCase()}`}>
      <div className="section-heading">
        <h3 id={`chart-${title.replaceAll(' ', '-').toLowerCase()}`}>{title}</h3>
        <strong>{formatValue(latest)}</strong>
      </div>
      {valid.length === 0 ? (
        <p className="empty">{t('noRetainedValues')}</p>
      ) : (
        <>
          <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={t('chartTrend', { title, count: valid.length })}>
            <path d={line} fill="none" stroke="currentColor" strokeWidth="3" vectorEffect="non-scaling-stroke" />
          </svg>
          <details>
            <summary>{t('accessibleDataTable')}</summary>
            <div className="table-scroll">
              <table>
                <thead><tr><th scope="col">{t('time')}</th><th scope="col">{t('value')}</th></tr></thead>
                <tbody>
                  {points.slice(-24).map((point) => (
                    <tr key={point.at}><td>{formatTime(point.at, language)}</td><td>{formatValue(point.value)}</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        </>
      )}
    </section>
  )
}
