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
    value === 'cancelled' ? ('cancelled' as const) : undefined,
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
