import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { MetricChart } from './MetricChart'
import { I18nProvider } from '../i18n'

describe('MetricChart', () => {
  it('positions mixed-resolution samples by elapsed time when requested', () => {
    const markup = renderToStaticMarkup(<I18nProvider><MetricChart timeScale title="TCP latency" points={[
      { at: '2026-10-11T00:00:00Z', value: 10 },
      { at: '2026-10-11T00:01:00Z', value: 10 },
      { at: '2026-10-11T01:00:00Z', value: 10 },
    ]} formatValue={(value) => `${value} ms`} /></I18nProvider>)
    expect(markup).toContain('d="M 0.0 12.0 L 10.7 12.0 L 640.0 12.0"')
    expect(markup).toContain('<table>')
  })

  it('keeps a single timed sample finite without a duration', () => {
    const markup = renderToStaticMarkup(<I18nProvider><MetricChart timeScale title="TCP latency" points={[
      { at: '2026-10-11T00:00:00Z', value: 10 },
    ]} formatValue={(value) => `${value} ms`} /></I18nProvider>)
    expect(markup).toContain('d="M 0.0 12.0"')
    expect(markup).not.toContain('NaN')
  })

  it('provides a named graphic and keyboard-accessible text table alternative', () => {
    const markup = renderToStaticMarkup(<I18nProvider><MetricChart title="CPU history" points={[
      { at: '2026-10-11T00:00:00Z', value: 10 }, { at: '2026-10-11T00:01:00Z', value: 20 },
    ]} formatValue={(value) => `${value ?? 0}%`} /></I18nProvider>)
    expect(markup).toContain('role="img"')
    expect(markup).toContain('Accessible data table')
    expect(markup).toContain('<table>')
    expect(markup).toContain('scope="col"')
  })
})
