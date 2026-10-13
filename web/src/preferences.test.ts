import { afterEach, describe, expect, it, vi } from 'vitest'

import { resolveInitialLanguage, translate } from './i18n'
import { loadSiteTheme, resolveInitialTheme, resolveVisualOverride } from './theme'

afterEach(() => vi.unstubAllGlobals())

describe('persisted display preferences', () => {
  it('uses valid versioned preferences', () => {
    expect(resolveInitialLanguage('{"version":1,"value":"zh"}', 'en-US')).toBe('zh')
    expect(resolveInitialTheme('{"version":1,"value":"light"}', false)).toBe('light')
  })

  it('falls back safely for malformed or obsolete values', () => {
    expect(resolveInitialLanguage('zh', 'zh-HK')).toBe('zh')
    expect(resolveInitialLanguage('{"version":2,"value":"zh"}', 'en-US')).toBe('en')
    expect(resolveInitialLanguage('{"version":1,"value":"fr"}', 'zh-CN')).toBe('zh')
    expect(resolveInitialTheme('{"version":1,"value":"sepia"}', true)).toBe('light')
    expect(resolveInitialTheme('{"version":2,"value":"light"}', false)).toBe('dark')
    expect(resolveInitialTheme('not-json', false)).toBe('dark')
  })

  it('provides typed Chinese copy and interpolation', () => {
    expect(translate('zh', 'administratorLogin')).toBe('管理员登录')
    expect(translate('zh', 'onlineCount', { online: 2, total: 3 })).toBe('2/3 在线')
  })

  it('validates visual overrides separately from existing light/dark preferences', () => {
    expect(resolveVisualOverride('{"version":1,"value":"classic"}')).toBe('classic')
    expect(resolveVisualOverride('{"version":1,"value":"emerald"}')).toBe('emerald')
    for (const value of [null, 'classic', '{"version":2,"value":"emerald"}', '{"version":1,"value":"dark"}', '{"version":1,"value":"external"}']) expect(resolveVisualOverride(value)).toBeNull()
    expect(resolveInitialTheme('{"version":1,"value":"light"}', false)).toBe('light')
  })

  it('loads the decoded site theme and falls back to Emerald for unavailable or invalid config', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ default_theme: 'classic' })))
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce(new Response(JSON.stringify({ default_theme: 'third-party' })))
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()
    expect(await loadSiteTheme(controller.signal)).toBe('classic')
    expect(await loadSiteTheme(controller.signal)).toBe('emerald')
    expect(await loadSiteTheme(controller.signal)).toBe('emerald')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/public/appearance', expect.objectContaining({ signal: controller.signal, credentials: 'same-origin' }))
  })
})
