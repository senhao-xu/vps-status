import { afterEach, describe, expect, it, vi } from 'vitest'

import { APIError } from '../../api/client'
import { revokeEnrollment } from './enrollmentActions'

afterEach(() => vi.unstubAllGlobals())

function setupResponses(...responses: Response[]) {
  vi.stubGlobal('document', { cookie: 'vps_csrf=csrf-token' })
  const fetchMock = vi.fn()
  for (const response of responses) fetchMock.mockResolvedValueOnce(response)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('enrollment revoke/regenerate conflict boundary', () => {
  it('refreshes the consumed node on concurrent registration instead of retrying deletion', async () => {
    const fetchMock = setupResponses(
      new Response(JSON.stringify({ error: { code: 'enrollment_consumed', message: 'Already used' } }), { status: 409 }),
      new Response(JSON.stringify({ id: 'enrollment-1', node_name: 'Edge', expires_at: '2026-10-13T12:00:00Z', status: 'online', node_id: 'node-1' })),
    )
    const result = await revokeEnrollment('enrollment-1', new AbortController().signal)
    expect(result).toMatchObject({ status: 'consumed', enrollment: { status: 'online', nodeId: 'node-1' } })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls.map((call) => call[1].method)).toEqual(['DELETE', 'GET'])
    expect(fetchMock.mock.calls[0]?.[1].headers.get('X-CSRF-Token')).toBe('csrf-token')
  })

  it('allows a replacement only after confirmed revocation', async () => {
    const fetchMock = setupResponses(new Response(null, { status: 204 }))
    expect(await revokeEnrollment('enrollment-1', new AbortController().signal)).toEqual({ status: 'revoked' })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('does not infer revocation from a missing record that may have been consumed and pruned', async () => {
    const fetchMock = setupResponses(new Response(JSON.stringify({ error: { code: 'not_found', message: 'Not found' } }), { status: 404 }))
    await expect(revokeEnrollment('enrollment-1', new AbortController().signal)).rejects.toMatchObject({ status: 404, code: 'not_found' })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('does not hide unauthorized failures', async () => {
    setupResponses(new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'Sign in' } }), { status: 401 }))
    await expect(revokeEnrollment('enrollment-1', new AbortController().signal)).rejects.toBeInstanceOf(APIError)
  })
})
