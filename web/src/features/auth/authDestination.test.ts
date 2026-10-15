import { describe, expect, it } from 'vitest'

import { returnHomeAfterAuth } from './authDestination'

describe('Homepage sign-in destination', () => {
  it('accepts the fixed homepage entry', () => {
    expect(returnHomeAfterAuth('/admin', '?next=home')).toBe(true)
  })

  it.each([
    ['/admin', ''], ['/admin', '?next=/'], ['/admin', '?next=https://example.com'],
    ['/admin', '?next=//example.com'], ['/admin', '?next=home&next=https://example.com'],
    ['/admin', '?next=home&other=1'], ['/admin', '?next=%68ome'],
    ['/admin/nodes', '?next=home'], ['/', '?next=home'], ['/public', '?next=home'],
  ])('keeps ordinary or untrusted entries in administration: %s%s', (pathname, search) => {
    expect(returnHomeAfterAuth(pathname, search)).toBe(false)
  })
})
