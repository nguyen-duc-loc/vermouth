import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  loadRuntimeConfig: vi.fn(async () => ({ googleAuthEnabled: false })),
  refreshSession: vi.fn(async () => null),
  render: vi.fn(),
}))

vi.mock('react-dom/client', () => ({
  createRoot: () => ({ render: mocks.render }),
}))

vi.mock('./api/runtime', () => ({ loadRuntimeConfig: mocks.loadRuntimeConfig }))
vi.mock('./api/session', () => ({ refreshSession: mocks.refreshSession }))
vi.mock('./routes', () => ({ router: {} }))

async function bootAt(pathname: string) {
  window.history.replaceState(null, '', pathname)
  document.body.innerHTML = '<div id="root"></div>'
  await import('./main')
}

describe('application boot', () => {
  beforeEach(() => {
    vi.resetModules()
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
})
