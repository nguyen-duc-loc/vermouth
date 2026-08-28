import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from '@tanstack/react-router'
import { lazy, Suspense } from 'react'

import { hasAccessToken } from './api/client'
import { SignInPage } from './pages/SignInPage'
import { ThreadPage } from './pages/ThreadPage'

const rootRoute = createRootRoute({
  component: () => (
    <div className="min-h-screen bg-background text-foreground">
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

function routeTree() {
  if (import.meta.env.DEV) {
    const DesignSystemPage = lazy(() =>
      import('./pages/DesignSystemPage').then((module) => ({ default: module.DesignSystemPage })),
    )
    const designSystemRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: '/design-system',
      component: () => (
        <Suspense
          fallback={
            <div
              role="status"
              className="grid min-h-screen place-items-center text-sm text-muted-foreground"
            >
              Đang mở thư viện giao diện…
            </div>
          }
        >
          <DesignSystemPage />
        </Suspense>
      ),
    })

    return rootRoute.addChildren([threadRoute, signInRoute, designSystemRoute])
  }

  return rootRoute.addChildren([threadRoute, signInRoute])
}

export const router = createRouter({
  routeTree: routeTree(),
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
