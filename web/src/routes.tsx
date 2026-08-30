import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from '@tanstack/react-router'
import { lazy, Suspense } from 'react'

import {
  cleanRedirect,
  cleanSignInError,
  type SignInErrorCode,
  sessionCoordinator,
} from './api/session'
import { ProtectedHomePage } from './pages/SessionStatePage'
import { SignInPage } from './pages/SignInPage'

type RouterContext = {
  session: typeof sessionCoordinator
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <div className="min-h-screen bg-background text-foreground">
      <Outlet />
    </div>
  ),
})

type SignInSearch = {
  error?: SignInErrorCode
  redirect: string
}

// The sign in screen keeps one known refusal and one clean relative target.
// Everything else in the URL is ignored rather than trusted.
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/signin',
  component: SignInPage,
  validateSearch: (search: Record<string, unknown>): SignInSearch => {
    const error = cleanSignInError(search.error)
    const redirectTo = cleanRedirect(search.redirect)
    return error ? { error, redirect: redirectTo } : { redirect: redirectTo }
  },
})

// The route waits for the shared boot refresh, so protected content never
// renders while the browser is still checking the cookie.
const threadRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: ProtectedHomePage,
  beforeLoad: async ({ context, location }) => {
    const session = await context.session.ensure()
    if (session.status === 'anonymous') {
      throw redirect({
        to: '/signin',
        search: { redirect: cleanRedirect(location.href) },
      })
    }
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
  context: { session: sessionCoordinator },
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
