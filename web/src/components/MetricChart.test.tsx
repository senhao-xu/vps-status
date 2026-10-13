import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { MetricChart } from './MetricChart'
import { I18nProvider } from '../i18n'

describe('MetricChart', () => {
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
