import { useNavigate } from '@tanstack/react-router'
import { LoaderCircle } from 'lucide-react'
import type { ReactNode } from 'react'
import { lazy, Suspense, useEffect } from 'react'

import { sessionCoordinator, useSession } from '../api/session'
import { ErrorState } from '../components/ErrorState'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader } from '../components/ui/card'

const HomePage = lazy(() => import('./HomePage').then((module) => ({ default: module.HomePage })))
const SchedulePage = lazy(() =>
  import('./SchedulePage').then((module) => ({ default: module.SchedulePage })),
)
const StudentsPage = lazy(() =>
  import('./StudentsPage').then((module) => ({ default: module.StudentsPage })),
)
const StudentDetailPage = lazy(() =>
  import('./StudentDetailPage').then((module) => ({ default: module.StudentDetailPage })),
)
const ClassDetailPage = lazy(() =>
  import('./ClassDetailPage').then((module) => ({ default: module.ClassDetailPage })),
)
const ProfilePage = lazy(() =>
  import('./ProfilePage').then((module) => ({ default: module.ProfilePage })),
)

/** Keeps protected content behind the current session state. */
export function ProtectedHomePage() {
  const session = useSession()
  const navigate = useNavigate()

  useEffect(() => {
    if (session.status === 'anonymous') {
      void navigate({ to: '/signin', search: { redirect: '/' } })
    }
    if (session.status === 'checking') document.title = 'Checking session · Vermouth'
    if (session.status === 'unavailable') document.title = 'Session unavailable · Vermouth'
  }, [navigate, session.status])

  if (session.status === 'checking') return <CheckingSession />
  if (session.status === 'unavailable') {
    return <UnavailableSession message={session.message} />
  }
  if (session.status === 'authenticated') {
    return (
      <Suspense fallback={<OpeningHomePage />}>
        <HomePage />
      </Suspense>
    )
  }
  return null
}

/** Keeps the calendar behind the same verified session boundary as home. */
export function ProtectedSchedulePage() {
  const session = useSession()
  const navigate = useNavigate()

  useEffect(() => {
    if (session.status === 'anonymous') {
      void navigate({ to: '/signin', search: { redirect: '/schedule' } })
    }
  }, [navigate, session.status])

  if (session.status === 'checking') return <CheckingSession />
  if (session.status === 'unavailable') return <UnavailableSession message={session.message} />
  if (session.status === 'authenticated') {
    return (
      <Suspense fallback={<OpeningHomePage />}>
        <SchedulePage />
      </Suspense>
    )
  }
  return null
}

/** Keeps student records behind the verified session boundary. */
export function ProtectedStudentsPage() {
  return <ProtectedLazyPage redirectTo="/students" page={<StudentsPage />} />
}

/** Keeps one student record behind the verified session boundary. */
export function ProtectedStudentDetailPage() {
  return <ProtectedLazyPage redirectTo="/students" page={<StudentDetailPage />} />
}

/** Keeps one class roster behind the verified session boundary. */
export function ProtectedClassDetailPage() {
  return <ProtectedLazyPage redirectTo="/schedule" page={<ClassDetailPage />} />
}

/** Keeps private invoice identity behind the verified session boundary. */
export function ProtectedProfilePage() {
  return <ProtectedLazyPage redirectTo="/profile" page={<ProfilePage />} />
}

function ProtectedLazyPage({ redirectTo, page }: { redirectTo: string; page: ReactNode }) {
  const session = useSession()
  const navigate = useNavigate()

  useEffect(() => {
    if (session.status === 'anonymous') {
      void navigate({ to: '/signin', search: { redirect: redirectTo } })
    }
  }, [navigate, redirectTo, session.status])

  if (session.status === 'checking') return <CheckingSession />
  if (session.status === 'unavailable') return <UnavailableSession message={session.message} />
  if (session.status === 'authenticated') {
    return <Suspense fallback={<OpeningHomePage />}>{page}</Suspense>
  }
  return null
}

/** Announces the short code split handoff after the session is accepted. */
export function OpeningHomePage() {
  return (
    <main className="grid min-h-screen place-items-center bg-background px-4 py-8 text-foreground">
      <Card
        role="status"
        aria-live="polite"
        aria-busy="true"
        className="w-full max-w-md shadow-raised"
      >
        <CardHeader>
          <div className="mb-2 grid size-12 place-items-center rounded-xl bg-primary/10 text-primary">
            <LoaderCircle
              aria-hidden="true"
              className="size-icon-lg animate-spin motion-reduce:animate-none"
            />
          </div>
          <h1 className="text-lg font-semibold">Opening your teaching day</h1>
        </CardHeader>
        <CardContent className="text-sm leading-relaxed text-muted-foreground">
          Vermouth is preparing today’s sessions and attendance controls.
        </CardContent>
      </Card>
    </main>
  )
}

/** Shows an announced loading surface while refresh is unresolved. */
export function CheckingSession() {
  return (
    <main className="grid min-h-screen place-items-center bg-background px-4 py-8 text-foreground">
      <Card
        role="status"
        aria-live="polite"
        aria-busy="true"
        className="w-full max-w-md shadow-raised"
      >
        <CardHeader>
          <div className="mb-2 grid size-12 place-items-center rounded-xl bg-primary/10 text-primary">
            <LoaderCircle
              aria-hidden="true"
              className="size-icon-lg animate-spin motion-reduce:animate-none"
            />
          </div>
          <h1 className="text-lg font-semibold">Checking your session</h1>
        </CardHeader>
        <CardContent className="text-sm leading-relaxed text-muted-foreground">
          Vermouth is confirming this device before opening your teaching day.
        </CardContent>
      </Card>
    </main>
  )
}

/** Gives a failed refresh or sign out one clear recovery action. */
export function UnavailableSession({ message }: { message?: string }) {
  return (
    <main className="grid min-h-screen place-items-center bg-background px-4 py-8 text-foreground">
      <div className="grid w-full max-w-md gap-4">
        <header className="grid gap-2">
          <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">Vermouth</p>
          <h1 className="text-2xl font-semibold text-balance">Your session needs another try.</h1>
        </header>
        <ErrorState
          headingLevel="h2"
          title="Vermouth could not confirm this device"
          description={message ?? 'Check the connection, then try again.'}
          action={
            <Button variant="secondary" onClick={() => void sessionCoordinator.retry()}>
              Try again
            </Button>
          }
        />
      </div>
    </main>
  )
}
