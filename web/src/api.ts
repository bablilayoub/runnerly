import type { Command, EnrollmentToken, Overview, Runner, RunnerEvent, User } from './types'

/** ApiError carries what the server said, including its hint. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly hint: string

  constructor(status: number, code: string, message: string, hint: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.hint = hint
  }

  /** Whether the caller should be sent to sign in again. */
  get isUnauthenticated(): boolean {
    return this.status === 401
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
    // The session is a cookie, so it has to ride along.
    credentials: 'same-origin',
  })

  if (!response.ok) {
    // Every failure from this API has the same shape, but a proxy in front
    // of it might not, so fall back to the status text.
    let code = 'request_failed'
    let message = `${response.status} ${response.statusText}`
    let hint = ''
    try {
      const body = await response.json()
      code = body.error ?? code
      message = body.message ?? message
      hint = body.hint ?? ''
    } catch {
      // Not JSON. The status is all we have.
    }
    throw new ApiError(response.status, code, message, hint)
  }

  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export interface AuthConfig {
  sign_in_available: boolean
  reason?: string
  hint?: string
}

export const api = {
  /** Public: whether sign-in can work at all on this server. */
  authConfig: () => request<AuthConfig>('/auth/config'),
  session: () => request<{ user: User }>('/auth/session'),
  logout: () => request<{ status: string }>('/auth/logout', { method: 'POST' }),

  overview: () => request<Overview>('/overview'),

  runners: () => request<{ runners: Runner[] }>('/runners'),
  runner: (id: string) => request<Runner>(`/runners/${encodeURIComponent(id)}`),
  deleteRunner: (id: string) =>
    request<{ deleted: string; note: string }>(`/runners/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  restartRunner: (id: string) =>
    request<{ command: Command; note: string }>(`/runners/${encodeURIComponent(id)}/restart`, {
      method: 'POST',
    }),
  commands: (id: string) =>
    request<{ commands: Command[] }>(`/runners/${encodeURIComponent(id)}/commands`),

  events: (params: { runnerId?: string; severity?: string; limit?: number } = {}) => {
    const query = new URLSearchParams()
    if (params.runnerId) query.set('runner_id', params.runnerId)
    if (params.severity) query.set('severity', params.severity)
    if (params.limit) query.set('limit', String(params.limit))
    const suffix = query.toString() ? `?${query}` : ''
    return request<{ events: RunnerEvent[] }>(`/events${suffix}`)
  },

  enrollmentTokens: () => request<{ tokens: EnrollmentToken[] }>('/enrollment-tokens'),
  createEnrollmentToken: (body: {
    description?: string
    max_uses?: number | null
    expires_in_hours?: number
  }) =>
    request<{ token: EnrollmentToken; secret: string; note: string }>('/enrollment-tokens', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  revokeEnrollmentToken: (id: string) =>
    request<{ revoked: string }>(`/enrollment-tokens/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
}
