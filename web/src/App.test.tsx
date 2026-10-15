import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'

describe('App', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders the public homepage without browser globals', () => {
    const markup = renderToStaticMarkup(<App />)

    expect(markup).toContain('Loading status')
    expect(markup).toContain('<a href="/admin">Management</a>')
    expect(markup).not.toContain('Checking Control session')
    expect(markup).toContain('role="status"')
    expect(markup).toContain('Switch language to Chinese')
    expect(markup).toContain('Light theme')
  })

  it.each(['/', '/public', '/public/', '/public/fleet', '/administrator', '/admin-tools', '/other'])('keeps %s outside administrator authentication', (pathname) => {
    vi.stubGlobal('window', { location: { pathname }, navigator: { language: 'en' } })
    const markup = renderToStaticMarkup(<App />)

    expect(markup).toContain('Loading status')
    expect(markup).toContain('<a href="/admin">Management</a>')
    expect(markup).not.toContain('Checking Control session')
    expect(markup).not.toContain('auth-shell')
  })

  it.each(['/admin', '/admin/', '/admin/nodes'])('requires administrator authentication at %s', (pathname) => {
    vi.stubGlobal('window', { location: { pathname }, navigator: { language: 'en' } })
    const markup = renderToStaticMarkup(<App />)

    expect(markup).toContain('Checking Control session')
    expect(markup).toContain('auth-shell')
    expect(markup).not.toContain('Loading status')
    expect(markup).not.toContain('public-shell')
  })
})
