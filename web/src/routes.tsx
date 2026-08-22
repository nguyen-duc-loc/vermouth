import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from '@tanstack/react-router'

import { hasAccessToken } from './api/client'
import { SignInPage } from './pages/SignInPage'
import { ThreadPage } from './pages/ThreadPage'

const rootRoute = createRootRoute({
  component: () => (
    <div className="min-h-screen bg-neutral-50 text-neutral-900">
      <Outlet />
    </div>
  ),
})

// The sign in screen carries one search param, the refusal code the callback
// redirected with. Anything else in the URL is ignored rather than trusted.
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/signin',
  component: SignInPage,
  validateSearch: (search: Record<string, unknown>): { error?: string } =>
    typeof search.error === 'string' ? { error: search.error } : {},
})

// Whether the app is signed in is whether the boot time refresh in main.tsx got a
// token, never a stored flag (spec 0004).
const threadRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: ThreadPage,
  beforeLoad: () => {
    if (!hasAccessToken()) throw redirect({ to: '/signin' })
  },
})

export const router = createRouter({
  routeTree: rootRoute.addChildren([threadRoute, signInRoute]),
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
