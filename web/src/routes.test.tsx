import { beforeEach, describe, expect, it, vi } from 'vitest'

const auth = vi.hoisted(() => ({ hasAccessToken: false }))

vi.mock('./api/client', () => ({
  hasAccessToken: () => auth.hasAccessToken,
}))

import { router } from './routes'

type RouteForTest = {
  options: {
    beforeLoad?: () => unknown
    validateSearch?: (search: Record<string, unknown>) => unknown
  }
}

const routes = router.routesByPath as unknown as Record<string, RouteForTest>

describe('route tree', () => {
  beforeEach(() => {
    auth.hasAccessToken = false
  })

  it('AC-8 registers the gallery in development', () => {
    expect(routes['/design-system']).toBeDefined()
  })

  it('keeps only a string refusal code from sign in search values', () => {
    expect(
      routes['/signin']?.options.validateSearch?.({ error: 'cancelled', extra: 'ignored' }),
    ).toEqual({ error: 'cancelled' })
    expect(routes['/signin']?.options.validateSearch?.({ error: 42 })).toEqual({})
  })

  it('keeps the application route closed without an access token', () => {
    expect(() => routes['/']?.options.beforeLoad?.()).toThrow()

    auth.hasAccessToken = true

    expect(() => routes['/']?.options.beforeLoad?.()).not.toThrow()
  })
})
