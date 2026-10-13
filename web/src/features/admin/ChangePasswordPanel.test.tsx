import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider, translate } from '../../i18n'
import { ChangePasswordPanel } from './ChangePasswordPanel'

describe('ChangePasswordPanel', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('labels three private password inputs with native autofill and associated guidance', () => {
    const markup = renderToStaticMarkup(<I18nProvider><ChangePasswordPanel onUnauthorized={() => {}} /></I18nProvider>)

    expect(markup).toContain('Current password<input')
    expect(markup).toContain('New password<input')
    expect(markup).toContain('Confirm new password<input')
    expect(markup.match(/type="password"/g)).toHaveLength(3)
    expect(markup.match(/autoComplete="current-password"/g)).toHaveLength(1)
    expect(markup.match(/autoComplete="new-password"/g)).toHaveLength(2)
    const description = markup.match(/<p class="muted" id="([^"]+)"/)
    expect(description).not.toBeNull()
    expect(markup.match(new RegExp(`aria-describedby="${description?.[1]}"`, 'g'))).toHaveLength(3)
    expect(markup).toContain('Changing your password signs out all administrator sessions.')
    expect(markup).toContain('Your MFA settings stay the same.')
    expect(markup).toContain('aria-busy="false"')
    expect(markup).not.toMatch(/required|minLength|maxLength|value=/)
  })

  it('renders Chinese labels and preserves the same empty-password input contract', () => {
    vi.stubGlobal('window', { navigator: { language: 'zh-CN' } })
    const markup = renderToStaticMarkup(<I18nProvider><ChangePasswordPanel onUnauthorized={() => {}} /></I18nProvider>)

    expect(markup).toContain('修改密码')
    expect(markup).toContain('当前密码<input')
    expect(markup).toContain('新密码<input')
    expect(markup).toContain('确认新密码<input')
    expect(markup).not.toMatch(/required|minLength|maxLength/)
  })

  it('provides localized validation, credential errors, rate limits and the post-logout notice', () => {
    expect(translate('en', 'passwordsMismatch')).toBe('The new passwords do not match.')
    expect(translate('zh', 'passwordsMismatch')).toBe('两次输入的新密码不一致。')
    expect(translate('en', 'currentPasswordInvalid')).toBe('The current password is incorrect.')
    expect(translate('zh', 'currentPasswordInvalid')).toBe('当前密码不正确。')
    expect(translate('en', 'passwordAuthStateChanged')).toContain('Sign in again')
    expect(translate('zh', 'passwordAuthStateChanged')).toContain('重新登录')
    expect(translate('en', 'passwordRateLimited')).toContain('Try again later')
    expect(translate('zh', 'passwordRateLimited')).toContain('稍后再试')
    expect(translate('en', 'passwordChanged')).toBe('Password changed. Sign in with your new password.')
    expect(translate('zh', 'passwordChanged')).toBe('密码已修改，请使用新密码登录。')
  })
})
