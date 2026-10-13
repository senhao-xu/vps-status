import { describe, expect, it } from 'vitest'

import { translate, type TranslationKey } from '../i18n'
import { APIError, errorMessage } from './client'

const zh = (key: TranslationKey, values?: Record<string, string | number>) => translate('zh', key, values)

describe('errorMessage', () => {
  it('localizes client-generated failures while preserving server messages', () => {
    expect(errorMessage(new APIError(502, 'response_invalid', 'English fallback'), zh)).toBe('Control 返回了无效响应。')
    expect(errorMessage(new APIError(503, 'request_failed', 'English fallback'), zh)).toBe('请求失败（503）。')
    expect(errorMessage(new Error('decoder detail'), zh)).toBe('无法完成请求。')
    expect(errorMessage(new APIError(401, 'invalid_credentials', 'The credentials are invalid.'), zh)).toBe('The credentials are invalid.')
  })
})
