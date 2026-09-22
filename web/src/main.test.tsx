import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  clearBillingClientState: vi.fn(async () => undefined),
  clearDrafts: vi.fn(),
  clearProfileClientState: vi.fn(async () => undefined),
  loadRuntimeConfig: vi.fn(async () => ({ googleAuthEnabled: false })),
  refreshSession: vi.fn(async () => null),
  render: vi.fn(),
  sessionListener: undefined as (() => void) | undefined,
  sessionStatus: 'checking' as 'checking' | 'authenticated' | 'anonymous' | 'unavailable',
}))

vi.mock('react-dom/client', () => ({
  createRoot: () => ({ render: mocks.render }),
}))

vi.mock('./api/runtime', () => ({ loadRuntimeConfig: mocks.loadRuntimeConfig }))
vi.mock('./api/billing', () => ({ clearBillingClientState: mocks.clearBillingClientState }))
vi.mock('./api/profile', () => ({ clearProfileClientState: mocks.clearProfileClientState }))
vi.mock('./api/session', () => ({
  refreshSession: mocks.refreshSession,
  sessionCoordinator: {
    getSnapshot: () => ({ status: mocks.sessionStatus }),
    subscribe: (listener: () => void) => {
      mocks.sessionListener = listener
      return () => undefined
    },
  },
}))
vi.mock('./lib/teaching-draft', () => ({ clearAllTeachingDrafts: mocks.clearDrafts }))
vi.mock('./routes', () => ({ router: {} }))

async function bootAt(pathname: string) {
  window.history.replaceState(null, '', pathname)
  document.body.innerHTML = '<div id="root"></div>'
  await import('./main')
}

describe('application boot', () => {
  beforeEach(() => {
    vi.resetModules()
    mocks.clearBillingClientState.mockClear()
    mocks.clearDrafts.mockClear()
    mocks.clearProfileClientState.mockClear()
    mocks.sessionListener = undefined
    mocks.sessionStatus = 'checking'
  })

  it.each(['/design-system', '/design-system/'])(
    'AC-8 opens the development gallery at %s without an auth request',
    async (pathname) => {
      await bootAt(pathname)

      expect(mocks.loadRuntimeConfig).toHaveBeenCalledOnce()
      expect(mocks.refreshSession).not.toHaveBeenCalled()
      expect(mocks.render).toHaveBeenCalledOnce()
    },
  )

  it('keeps the session refresh on normal application routes', async () => {
    await bootAt('/signin')

    expect(mocks.refreshSession).toHaveBeenCalledOnce()
    expect(mocks.render).toHaveBeenCalledOnce()
  })

  // covers: spec 0013 AC-21
  it('clears billing state when the session becomes anonymous outside explicit sign out', async () => {
    await bootAt('/billing')
    mocks.sessionStatus = 'anonymous'

    mocks.sessionListener?.()

    expect(mocks.clearDrafts).toHaveBeenCalledOnce()
    expect(mocks.clearProfileClientState).toHaveBeenCalledOnce()
    expect(mocks.clearBillingClientState).toHaveBeenCalledOnce()
  })
})
