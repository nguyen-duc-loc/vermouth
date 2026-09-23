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
import {
  ProtectedBillingPage,
  ProtectedClassDetailPage,
  ProtectedHomePage,
  ProtectedProfilePage,
  ProtectedSchedulePage,
  ProtectedStudentDetailPage,
  ProtectedStudentsPage,
} from './pages/SessionStatePage'
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
type HomeSearch = {
  date?: string
  session?: string
}

const threadRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: ProtectedHomePage,
  validateSearch: (search: Record<string, unknown>): HomeSearch => ({
    date: validCalendarDate(search.date) ? search.date : undefined,
    session: validUUID(search.session) ? search.session : undefined,
  }),
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

type ScheduleSearch = {
  view?: 'day' | 'week' | 'month'
  date?: string
  classes: string[]
}

const scheduleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/schedule',
  component: ProtectedSchedulePage,
  validateSearch: (search: Record<string, unknown>): ScheduleSearch => ({
    view:
      search.view === 'day' || search.view === 'week' || search.view === 'month'
        ? search.view
        : undefined,
    date: validCalendarDate(search.date) ? search.date : undefined,
    classes: cleanClassSearch(search.classes),
  }),
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

type StudentsSearch = {
  q?: string
  cursor?: string
}

const studentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/students',
  component: ProtectedStudentsPage,
  validateSearch: (search: Record<string, unknown>): StudentsSearch => ({
    q: cleanOptionalSearchText(search.q, 160),
    cursor: cleanOptionalSearchText(search.cursor, 4096),
  }),
  beforeLoad: requireSession,
})

const studentDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/students/$studentId',
  component: ProtectedStudentDetailPage,
  beforeLoad: requireSession,
})

type ClassSearch = { date?: string; rateDate?: string }

const classDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/classes/$classId',
  component: ProtectedClassDetailPage,
  validateSearch: (search: Record<string, unknown>): ClassSearch => ({
    date: validCalendarDate(search.date) ? search.date : undefined,
    rateDate: validCalendarDate(search.rateDate) ? search.rateDate : undefined,
  }),
  beforeLoad: requireSession,
})

const profileRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/profile',
  component: ProtectedProfilePage,
  beforeLoad: requireSession,
})

type BillingSearch = {
  year?: number
  month?: number
}

const billingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/billing',
  component: ProtectedBillingPage,
  validateSearch: (search: Record<string, unknown>): BillingSearch => ({
    year: validBillingYear(search.year) ? Number(search.year) : undefined,
    month: validBillingMonth(search.month) ? Number(search.month) : undefined,
  }),
  beforeLoad: requireSession,
})

function validBillingYear(value: unknown) {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : 0
  return Number.isInteger(parsed) && parsed >= 2000
}

function validBillingMonth(value: unknown) {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : 0
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 12
}

async function requireSession({
  context,
  location,
}: {
  context: RouterContext
  location: { href: string }
}) {
  const session = await context.session.ensure()
  if (session.status === 'anonymous') {
    throw redirect({
      to: '/signin',
      search: { redirect: cleanRedirect(location.href) },
    })
  }
}

function cleanOptionalSearchText(value: unknown, maximum: number) {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed !== '' && [...trimmed].length <= maximum ? trimmed : undefined
}

function validCalendarDate(value: unknown): value is string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const parsed = new Date(`${value}T00:00:00Z`)
  return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value
}

function cleanClassSearch(value: unknown): string[] {
  const values = Array.isArray(value) ? value : typeof value === 'string' ? [value] : []
  return [...new Set(values.filter(validUUID))].sort()
}

function validUUID(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
  )
}

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

    return rootRoute.addChildren([
      threadRoute,
      scheduleRoute,
      studentsRoute,
      studentDetailRoute,
      classDetailRoute,
      billingRoute,
      profileRoute,
      signInRoute,
      designSystemRoute,
    ])
  }

  return rootRoute.addChildren([
    threadRoute,
    scheduleRoute,
    studentsRoute,
    studentDetailRoute,
    classDetailRoute,
    billingRoute,
    profileRoute,
    signInRoute,
  ])
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
