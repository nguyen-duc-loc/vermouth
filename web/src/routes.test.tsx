import { beforeEach, describe, expect, it, vi } from 'vitest'

const auth = vi.hoisted(() => ({
  status: 'anonymous' as 'anonymous' | 'authenticated',
}))

const session = vi.hoisted(() => ({
  ensure: vi.fn(async () => ({ status: auth.status })),
  getSnapshot: () => ({ status: auth.status }),
  retry: vi.fn(async () => undefined),
  subscribe: () => () => undefined,
}))

vi.mock('./api/session', () => ({
  cleanRedirect: (value: unknown) =>
    typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') ? value : '/',
  cleanSignInError: (value: unknown) =>
    value === 'cancelled' || value === 'rate_limited' ? value : undefined,
  sessionCoordinator: session,
  useSession: () => ({ status: auth.status }),
}))

import { router } from './routes'

type BeforeLoadInput = {
  context: { session: typeof session }
  location: { href: string }
}

type RouteForTest = {
  options: {
    beforeLoad?: (input: BeforeLoadInput) => Promise<unknown>
    validateSearch?: (search: Record<string, unknown>) => unknown
  }
}

const routes = router.routesByPath as unknown as Record<string, RouteForTest>

describe('route tree', () => {
  beforeEach(() => {
    auth.status = 'anonymous'
    session.ensure.mockClear()
  })

  it('AC-8 registers the gallery in development', () => {
    expect(routes['/design-system']).toBeDefined()
  })

  it('keeps one known refusal and a clean relative redirect', () => {
    expect(
      routes['/signin']?.options.validateSearch?.({
        error: 'cancelled',
        redirect: '/thread?day=today',
        extra: 'ignored',
      }),
    ).toEqual({ error: 'cancelled', redirect: '/thread?day=today' })
    expect(routes['/signin']?.options.validateSearch?.({ error: 42 })).toEqual({ redirect: '/' })
    expect(routes['/signin']?.options.validateSearch?.({ error: 'rate_limited' })).toEqual({
      error: 'rate_limited',
      redirect: '/',
    })
  })

  it('AC-13 and AC-14 keep only canonical class identifiers in schedule search', () => {
    const first = '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421'
    const second = '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422'

    expect(
      routes['/schedule']?.options.validateSearch?.({
        view: 'unknown',
        date: 'not-a-date',
        classes: [second, 'bad', first, second, 42],
      }),
    ).toEqual({ classes: [first, second], date: undefined, view: undefined })
    expect(routes['/schedule']?.options.validateSearch?.({ classes: 'bad' })).toEqual({
      classes: [],
      date: undefined,
      view: undefined,
    })
    expect(routes['/schedule']?.options.validateSearch?.({ date: '2026-02-31' })).toEqual({
      classes: [],
      date: undefined,
      view: undefined,
    })
  })

  it('AC-16 validates student search, roster dates, and protected detail routes', () => {
    expect(routes['/students']).toBeDefined()
    expect(routes['/students/$studentId']).toBeDefined()
    expect(routes['/classes/$classId']).toBeDefined()
    expect(
      routes['/students']?.options.validateSearch?.({
        q: '  Mai  ',
        cursor: '  opaque-cursor  ',
        ignored: true,
      }),
    ).toEqual({ q: 'Mai', cursor: 'opaque-cursor' })
    expect(
      routes['/students']?.options.validateSearch?.({ q: 'x'.repeat(161), cursor: 42 }),
    ).toEqual({ q: undefined, cursor: undefined })
    expect(routes['/classes/$classId']?.options.validateSearch?.({ date: '2026-08-30' })).toEqual({
      date: '2026-08-30',
    })
    expect(routes['/classes/$classId']?.options.validateSearch?.({ date: '2026-02-31' })).toEqual({
      date: undefined,
    })
  })

  it('waits for session checking and closes the application route when anonymous', async () => {
    const beforeLoad = routes['/']?.options.beforeLoad
    expect(beforeLoad).toBeDefined()

    await expect(
      beforeLoad?.({ context: { session }, location: { href: '/?day=today' } }),
    ).rejects.toBeDefined()

    auth.status = 'authenticated'

    await expect(
      beforeLoad?.({ context: { session }, location: { href: '/?day=today' } }),
    ).resolves.toBeUndefined()
  })
})
