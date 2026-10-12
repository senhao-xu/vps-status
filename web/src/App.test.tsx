import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { App } from './App'

describe('App', () => {
  it('renders the session-checking state without browser globals', () => {
    const markup = renderToStaticMarkup(<App />)

    expect(markup).toContain('Checking Control session')
    expect(markup).toContain('role="status"')
  })
})
