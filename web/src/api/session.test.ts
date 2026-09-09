import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  post: vi.fn(),
  setAccessToken: vi.fn(),
}))

vi.mock('./client', () => ({
  api: { POST: client.post },
  setAccessToken: client.setAccessToken,
}))

function response(status: number, headers?: HeadersInit): Response {
  return new Response(null, { status, headers })
}

function session(expiresInMs = 120_000) {
  return {
    access_token: 'access-token',
    access_expires_at: new Date(Date.now() + expiresInMs).toISOString(),
  }
}

beforeEach(() => {
  vi.resetModules()
  client.post.mockReset()
  client.setAccessToken.mockReset()
  Object.defineProperty(navigator, 'languages', {
    configurable: true,
    value: ['vi-VN'],
  })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('session input cleaning', () => {
  // covers: AC-16
  it('keeps only the known sign in errors', async () => {
    const { cleanSignInError } = await import('./session')

    for (const known of [
      'not_allowed',
      'email_conflict',
      'cancelled',
      'expired_state',
      'provider_error',
      'rate_limited',
    ]) {
      expect(cleanSignInError(known)).toBe(known)
    }
    expect(cleanSignInError('unknown')).toBeUndefined()
    expect(cleanSignInError(null)).toBeUndefined()
  })

  // covers: AC-15
  it.each([
    ['empty', '', '/'],
    ['protocol relative', '//evil.example', '/'],
    ['backslash', String.raw`/\evil.example`, '/'],
    ['encoded protocol relative', '/%2f%2fevil.example', '/'],
    ['encoded backslash', '/%5cevil.example', '/'],
    ['full URL', 'https://evil.example/thread', '/'],
    ['control character', '/thread\nnext', '/'],
    ['fragment', '/thread#secret', '/'],
    ['invalid escape', '/%zz', '/'],
    ['clean path', '/thread', '/thread'],
    ['clean query', '/thread?day=1', '/thread?day=1'],
  ])('cleans %s redirects', async (_name, raw, want) => {
    const { cleanRedirect } = await import('./session')

    expect(cleanRedirect(raw)).toBe(want)
  })

  // covers: AC-10
  it('uses the first supported browser language', async () => {
    Object.defineProperty(navigator, 'languages', {
      configurable: true,
      value: ['fr-FR', 'en-GB', 'vi-VN'],
    })
    const { browserLanguage } = await import('./session')

    expect(browserLanguage()).toBe('en')
  })

  // covers: AC-10, AC-15
  it('builds the Google start URL from cleaned browser values', async () => {
    Object.defineProperty(navigator, 'languages', {
      configurable: true,
      value: ['en-US'],
    })
    const { googleSignInUrl } = await import('./session')

    const target = new URL(googleSignInUrl('//evil.example'), 'https://app.example')

    expect(target.pathname).toBe('/api/auth/google/start')
    expect(target.searchParams.get('lang')).toBe('en')
    expect(target.searchParams.get('redirect_to')).toBe('/')
    expect(target.searchParams.get('tz')).not.toBe('')
  })
})

describe('session coordinator', () => {
  // covers: AC-3
  it('shares one refresh request between concurrent callers', async () => {
    let resolveRefresh: ((value: unknown) => void) | undefined
    client.post.mockReturnValue(
      new Promise((resolve) => {
        resolveRefresh = resolve
      }),
    )
    const { sessionCoordinator } = await import('./session')

    const first = sessionCoordinator.refresh()
    const second = sessionCoordinator.refresh()

    expect(first).toBe(second)
    expect(client.post).toHaveBeenCalledTimes(1)
    resolveRefresh?.({ data: session(), response: response(200) })
    await expect(first).resolves.toMatchObject({
      status: 'signed_in',
      session: { access_token: 'access-token' },
    })
    expect(client.setAccessToken).toHaveBeenCalledWith('access-token')
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'authenticated' })
  })

  // covers: AC-3, AC-16
  it('refreshes and retries one protected request after its first unauthorized response', async () => {
    client.post.mockResolvedValue({ data: session(), response: response(200) })
    const { withProtectedRetry } = await import('./session')
    const request = vi
      .fn()
      .mockResolvedValueOnce({ response: response(401) })
      .mockResolvedValueOnce({ data: { ok: true }, response: response(200) })

    const result = await withProtectedRetry(request)

    expect(request).toHaveBeenCalledTimes(2)
    expect(result.data).toEqual({ ok: true })
  })

  // covers: AC-3, AC-16
  it('refuses the session after the retried protected request is still unauthorized', async () => {
    client.post.mockResolvedValue({ data: session(), response: response(200) })
    const { sessionCoordinator, withProtectedRetry } = await import('./session')
    const request = vi.fn().mockResolvedValue({ response: response(401) })

    await withProtectedRetry(request)

    expect(request).toHaveBeenCalledTimes(2)
    expect(client.setAccessToken).toHaveBeenLastCalledWith(null)
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'anonymous' })
  })

  // covers: AC-6
  it('clears memory before sign out finishes', async () => {
    let finishSignOut: ((value: unknown) => void) | undefined
    client.post.mockReturnValue(
      new Promise((resolve) => {
        finishSignOut = resolve
      }),
    )
    const { sessionCoordinator } = await import('./session')

    const pending = sessionCoordinator.signOut()

    expect(client.setAccessToken).toHaveBeenCalledWith(null)
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'checking' })
    finishSignOut?.({ response: response(204) })
    await expect(pending).resolves.toEqual({ status: 'anonymous' })
  })

  // covers: AC-6
  it('does not restore access when an older refresh finishes after sign out', async () => {
    let finishRefresh: ((value: unknown) => void) | undefined
    client.post
      .mockReturnValueOnce(
        new Promise((resolve) => {
          finishRefresh = resolve
        }),
      )
      .mockResolvedValueOnce({ response: response(204) })
    const { sessionCoordinator } = await import('./session')

    const pendingRefresh = sessionCoordinator.refresh()
    await sessionCoordinator.signOut()
    finishRefresh?.({ data: session(), response: response(200) })
    await pendingRefresh

    expect(client.setAccessToken).toHaveBeenLastCalledWith(null)
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'anonymous' })
  })

  // covers: AC-3, AC-16
  it('becomes anonymous when boot refresh says the session is over', async () => {
    client.post.mockResolvedValue({ response: response(401) })
    const { sessionCoordinator } = await import('./session')

    await expect(sessionCoordinator.start()).resolves.toEqual({ status: 'signed_out' })

    expect(client.setAccessToken).toHaveBeenLastCalledWith(null)
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'anonymous' })
  })

  // covers: AC-3, AC-16
  it('shows unavailable when boot refresh cannot reach the server', async () => {
    client.post.mockRejectedValue(new Error('network unavailable'))
    const { sessionCoordinator } = await import('./session')

    await expect(sessionCoordinator.start()).resolves.toEqual({ status: 'signed_out' })

    expect(client.setAccessToken).toHaveBeenLastCalledWith(null)
    expect(sessionCoordinator.getSnapshot()).toMatchObject({ status: 'unavailable' })
  })

  // covers: AC-12
  it('keeps auth memory unchanged and waits for manual retry after a limited boot refresh', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-04T08:00:00Z'))
    client.post
      .mockResolvedValueOnce({ response: response(429, { 'Retry-After': '3' }) })
      .mockResolvedValueOnce({ data: session(), response: response(200) })
    const { sessionCoordinator } = await import('./session')

    await expect(sessionCoordinator.start()).resolves.toEqual({
      status: 'rate_limited',
      retry_at: Date.parse('2026-09-04T08:00:03Z'),
    })
    expect(client.setAccessToken).not.toHaveBeenCalled()
    expect(sessionCoordinator.getSnapshot()).toEqual({
      status: 'anonymous',
      rateLimitedUntil: Date.parse('2026-09-04T08:00:03Z'),
    })

    await sessionCoordinator.retry()
    await vi.advanceTimersByTimeAsync(3_000)
    expect(client.post).toHaveBeenCalledTimes(1)

    await sessionCoordinator.retry()
    expect(client.post).toHaveBeenCalledTimes(2)
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'authenticated' })
  })

  // covers: AC-12
  it('preserves a current access token when renewal is rate limited', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-04T08:00:00Z'))
    client.post
      .mockResolvedValueOnce({ data: session(120_000), response: response(200) })
      .mockResolvedValueOnce({ response: response(429, { 'Retry-After': '30' }) })
    const { sessionCoordinator } = await import('./session')

    await sessionCoordinator.refresh()
    await sessionCoordinator.refresh()

    expect(client.setAccessToken).toHaveBeenCalledTimes(1)
    expect(client.setAccessToken).toHaveBeenCalledWith('access-token')
    expect(sessionCoordinator.getSnapshot()).toEqual({
      status: 'authenticated',
      rateLimitedUntil: Date.parse('2026-09-04T08:00:30Z'),
    })
  })

  // covers: AC-12
  it('does not automatically retry a limited renewal', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-04T08:00:00Z'))
    client.post
      .mockResolvedValueOnce({ data: session(120_000), response: response(200) })
      .mockResolvedValueOnce({ response: response(429, { 'Retry-After': '30' }) })
      .mockResolvedValueOnce({ data: session(120_000), response: response(200) })
    const { sessionCoordinator } = await import('./session')

    await sessionCoordinator.refresh()
    await sessionCoordinator.refresh()

    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(120_000)

    expect(client.post).toHaveBeenCalledTimes(2)
  })

  // covers: AC-3
  it('renews sixty seconds before the access token expires', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-29T12:00:00Z'))
    client.post.mockResolvedValue({ data: session(120_000), response: response(200) })
    const { sessionCoordinator } = await import('./session')

    await sessionCoordinator.refresh()
    await vi.advanceTimersByTimeAsync(59_999)
    expect(client.post).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(client.post).toHaveBeenCalledTimes(2)
  })

  // covers: AC-3
  it('keeps valid access during a transient failure and retries after one second', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-29T12:00:00Z'))
    client.post
      .mockResolvedValueOnce({ data: session(120_000), response: response(200) })
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce({ data: session(120_000), response: response(200) })
    const { sessionCoordinator } = await import('./session')

    await sessionCoordinator.refresh()
    await sessionCoordinator.refresh()

    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'authenticated' })
    await vi.advanceTimersByTimeAsync(999)
    expect(client.post).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(client.post).toHaveBeenCalledTimes(3)
  })

  // covers: AC-6
  it('offers a retry when sign out cannot reach the server', async () => {
    client.post
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce({ response: response(204) })
    const { sessionCoordinator } = await import('./session')

    await sessionCoordinator.signOut()
    expect(sessionCoordinator.getSnapshot()).toMatchObject({ status: 'unavailable' })

    await sessionCoordinator.retry()
    expect(sessionCoordinator.getSnapshot()).toEqual({ status: 'anonymous' })
  })
})
