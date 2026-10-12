import { decodeAPIError, type Decoder } from './contracts'

export class APIError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'APIError'
  }
}

function csrfCookie(): string {
  for (const item of document.cookie.split(';')) {
    const [name, ...parts] = item.trim().split('=')
    if (name === 'vps_csrf') return decodeURIComponent(parts.join('='))
  }
  return ''
}

async function responseJSON(response: Response): Promise<unknown> {
  try {
    return await response.json()
  } catch {
    throw new APIError(response.status, 'response_invalid', 'Control returned an invalid response.')
  }
}

export async function apiRequest<T>(
  path: string,
  decoder: Decoder<T>,
  options: { method?: string; body?: unknown; signal?: AbortSignal; csrf?: string } = {},
): Promise<T> {
  const headers = new Headers({ Accept: 'application/json' })
  if (options.body !== undefined) headers.set('Content-Type', 'application/json')
  const method = options.method ?? 'GET'
  if (!['GET', 'HEAD'].includes(method)) {
    const csrf = options.csrf ?? csrfCookie()
    if (csrf) headers.set('X-CSRF-Token', csrf)
  }
  const response = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  })
  if (!response.ok) {
    const payload = await responseJSON(response)
    try {
      const error = decodeAPIError(payload)
      throw new APIError(response.status, error.code, error.message)
    } catch (error) {
      if (error instanceof APIError) throw error
      throw new APIError(response.status, 'request_failed', `Request failed (${response.status}).`)
    }
  }
  return decoder(await responseJSON(response))
}

export async function apiMutation(
  path: string,
  options: { method?: string; body?: unknown; csrf?: string } = {},
): Promise<void> {
  const headers = new Headers()
  if (options.body !== undefined) headers.set('Content-Type', 'application/json')
  const csrf = options.csrf ?? csrfCookie()
  if (csrf) headers.set('X-CSRF-Token', csrf)
  const response = await fetch(path, {
    method: options.method ?? 'POST',
    headers,
    credentials: 'same-origin',
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })
  if (!response.ok) {
    const payload = await responseJSON(response)
    try {
      const error = decodeAPIError(payload)
      throw new APIError(response.status, error.code, error.message)
    } catch (error) {
      if (error instanceof APIError) throw error
      throw new APIError(response.status, 'request_failed', `Request failed (${response.status}).`)
    }
  }
}

export function errorMessage(error: unknown): string {
  if (error instanceof APIError || error instanceof Error) return error.message
  return 'The request could not be completed.'
}
