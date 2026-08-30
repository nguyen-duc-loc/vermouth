import {
  type InfiniteData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { BookOpen, CalendarDays, Home, LogOut, Plus, Sparkles } from 'lucide-react'
import { type MouseEvent as ReactMouseEvent, useEffect, useMemo, useRef, useState } from 'react'

import { signOut } from '../api/session'
import {
  createClass,
  createStudent,
  type Home as HomeData,
  joinRoster,
  markAttendance,
  readBillingProjection,
  readHome,
} from '../api/teaching'
import { AppearancePanel, type AppearancePanelText } from '../components/AppearancePanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { BillingProjectionPanel } from '../components/BillingProjectionPanel'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PageEntrance } from '../components/PageEntrance'
import { SessionAttendanceCard } from '../components/SessionAttendanceCard'
import { TeachingSetupSheet } from '../components/TeachingSetupSheet'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader } from '../components/ui/card'
import { Separator } from '../components/ui/separator'
import { Skeleton } from '../components/ui/skeleton'
import { clearAllTeachingDrafts } from '../lib/teaching-draft'

const homeQueryFamily = ['home'] as const
const homeQueryKey = ['home', null] as const
const billingQueryKey = ['home', 'billing-projection'] as const

const appearanceText: AppearancePanelText = {
  title: 'Appearance',
  themeLegend: 'Theme',
  accentLegend: 'Accent color',
  themes: { light: 'Light', dark: 'Dark', system: 'System' },
  accents: {
    red: 'Red',
    rose: 'Rose',
    orange: 'Orange',
    green: 'Green',
    blue: 'Blue',
    yellow: 'Yellow',
    violet: 'Violet',
  },
}

const destinations: readonly AppDestination[] = [{ href: '/', label: 'Home', icon: Home }]

/** Opens the tutor's local day with setup, attendance, and isolated projection progress. */
export function HomePage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [setupOpen, setSetupOpen] = useState(false)
  const [setupMessage, setSetupMessage] = useState<string>()
  const [savedMessage, setSavedMessage] = useState<{ sessionId: string; message: string }>()
  const [signingOut, setSigningOut] = useState(false)
  const [pollTimedOut, setPollTimedOut] = useState(false)
  const [pollCycle, setPollCycle] = useState(0)
  const [manualPolling, setManualPolling] = useState(false)
  const setupReturnFocusRef = useRef<HTMLButtonElement>(null)

  const homeQuery = useInfiniteQuery({
    queryKey: homeQueryKey,
    queryFn: ({ pageParam, signal }) => readHome(pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    // The explicit focus effect below resets the cursor chain before refetching.
    refetchOnWindowFocus: false,
  })
  const firstPage = homeQuery.data?.pages[0]
  const sessions = useMemo(
    () => homeQuery.data?.pages.flatMap((page) => page.sessions) ?? [],
    [homeQuery.data],
  )

  const billingQuery = useQuery({
    queryKey: billingQueryKey,
    queryFn: ({ signal }) => readBillingProjection(signal),
    enabled: (firstPage?.billing_projection?.state === 'waiting' || manualPolling) && !pollTimedOut,
    refetchInterval: (query) => {
      const active = query.state.data?.billing_projection?.state === 'active'
      return active || pollTimedOut ? false : 1000
    },
  })
  const projection = billingQuery.data
    ? billingQuery.data.billing_projection
    : firstPage?.billing_projection
  const projectionUnavailable = billingQuery.data
    ? billingQuery.data.billing_projection_unavailable
    : firstPage?.billing_projection_unavailable
  const waitingForProjection = projection?.state === 'waiting'
  const projectionPollWindow = waitingForProjection ? pollCycle : -1

  useEffect(() => {
    if (projectionPollWindow < 0) {
      setPollTimedOut(false)
      if (projection?.state === 'active') setManualPolling(false)
      return
    }
    setPollTimedOut(false)
    const timer = window.setTimeout(() => setPollTimedOut(true), 10_000)
    return () => window.clearTimeout(timer)
  }, [projection?.state, projectionPollWindow])

  useEffect(() => {
    const midnight = firstPage?.next_local_midnight_at
    if (!midnight) return
    const delay = Math.max(0, new Date(midnight).getTime() - Date.now())
    const timer = window.setTimeout(() => {
      void queryClient.resetQueries({ queryKey: homeQueryKey, exact: true })
    }, delay)
    return () => window.clearTimeout(timer)
  }, [firstPage?.next_local_midnight_at, queryClient])

  useEffect(() => {
    const resetHome = () => {
      void queryClient.resetQueries({ queryKey: homeQueryKey, exact: true })
    }
    window.addEventListener('focus', resetHome)
    return () => window.removeEventListener('focus', resetHome)
  }, [queryClient])

  useEffect(() => {
    document.title = 'Today · Vermouth'
  }, [])

  function refreshHomeAndProjection() {
    setPollCycle((current) => current + 1)
    void queryClient.resetQueries({ queryKey: homeQueryKey, exact: true })
    void queryClient.invalidateQueries({ queryKey: billingQueryKey, exact: true })
  }

  function openSetup(event: ReactMouseEvent<HTMLButtonElement>) {
    setupReturnFocusRef.current = event.currentTarget
    setSetupOpen(true)
  }

  const classMutation = useMutation({
    mutationFn: ({ input, key }: { input: Parameters<typeof createClass>[0]; key: string }) =>
      createClass(input, key),
    onSuccess: refreshHomeAndProjection,
  })
  const studentMutation = useMutation({
    mutationFn: ({ input, key }: { input: Parameters<typeof createStudent>[0]; key: string }) =>
      createStudent(input, key),
    onSuccess: refreshHomeAndProjection,
  })
  const rosterMutation = useMutation({
    mutationFn: ({
      classId,
      input,
    }: {
      classId: string
      input: Parameters<typeof joinRoster>[1]
    }) => joinRoster(classId, input),
    onSuccess: refreshHomeAndProjection,
  })
  const attendanceMutation = useMutation({
    mutationFn: ({
      sessionId,
      studentId,
      state,
    }: {
      sessionId: string
      studentId: string
      state: 'Present' | 'Absent'
    }) => markAttendance(sessionId, studentId, { state }),
    onSuccess: (attendance) => {
      queryClient.setQueryData<InfiniteData<HomeData>>(homeQueryKey, (current) => {
        if (!current) return current
        return {
          ...current,
          pages: current.pages.map((page) => ({
            ...page,
            sessions: page.sessions.map((session) =>
              session.session_id !== attendance.session_id
                ? session
                : {
                    ...session,
                    students: session.students.map((student) =>
                      student.student_id !== attendance.student_id
                        ? student
                        : {
                            ...student,
                            attendance_state: attendance.state,
                            marked_at: attendance.marked_at,
                          },
                    ),
                  },
            ),
          })),
        }
      })
      setSavedMessage({
        sessionId: attendance.session_id,
        message: `Attendance saved as ${attendance.state}.`,
      })
      setPollCycle((current) => current + 1)
      void queryClient.invalidateQueries({ queryKey: homeQueryFamily })
    },
  })

  const accountPanel = (
    <div className="grid gap-6">
      <AppearancePanel text={appearanceText} />
      <Separator />
      <Button
        variant="secondary"
        loading={signingOut}
        onClick={async () => {
          setSigningOut(true)
          clearAllTeachingDrafts()
          const session = await signOut()
          if (session.status === 'anonymous') {
            await navigate({ to: '/signin', search: { redirect: '/' } })
            return
          }
          setSigningOut(false)
        }}
      >
        <LogOut aria-hidden="true" className="size-icon-sm" />
        {signingOut ? 'Signing out…' : 'Sign out'}
      </Button>
    </div>
  )

  const billingPanel = (
    <BillingProjectionPanel
      projection={projection}
      unavailable={projectionUnavailable}
      timedOut={pollTimedOut}
      refreshing={billingQuery.isFetching}
      onRetry={() => {
        setPollTimedOut(false)
        setManualPolling(true)
        setPollCycle((current) => current + 1)
      }}
    />
  )

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to today',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Change this device appearance or end the current session.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={accountPanel}
      contextualPanel={firstPage ? billingPanel : undefined}
    >
      <PageEntrance className="mx-auto grid w-full max-w-6xl gap-8 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
        >
          <div className="grid max-w-3xl gap-3">
            <Badge variant="primary" className="w-fit">
              <CalendarDays aria-hidden="true" className="size-icon-sm" />
              {firstPage ? formatLocalDate(firstPage.local_date) : 'Your teaching day'}
            </Badge>
            <h1 className="text-3xl font-semibold text-balance">Today stays in one calm place.</h1>
            <p className="text-base leading-relaxed text-muted-foreground">
              Create the first class, then mark each student beside the session you are teaching.
            </p>
          </div>
          <Button
            size="large"
            disabled={!firstPage}
            onClick={openSetup}
            className="w-full lg:w-auto"
          >
            <Plus aria-hidden="true" className="size-icon-md" />
            Create a class
          </Button>
        </header>

        <div
          role="status"
          aria-live="polite"
          className="text-sm font-medium text-success-foreground"
        >
          {setupMessage}
        </div>

        {homeQuery.isPending ? (
          <Card role="status" aria-label="Loading today's sessions" aria-busy="true">
            <CardHeader className="gap-3">
              <Skeleton className="h-6 w-2/5" />
              <Skeleton className="h-4 w-3/4" />
            </CardHeader>
            <CardContent className="grid gap-3">
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
            </CardContent>
          </Card>
        ) : homeQuery.error ? (
          <ErrorState
            headingLevel="h2"
            title="Today could not be read"
            description={homeQuery.error.message}
            action={
              <Button variant="secondary" onClick={() => void homeQuery.refetch()}>
                Try today again
              </Button>
            }
          />
        ) : (
          <section data-entrance-item aria-labelledby="sessions-heading" className="grid gap-4">
            <header className="grid gap-1">
              <h2 id="sessions-heading" className="text-xl font-semibold">
                Today's sessions
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                Sessions stay chronological. Saved attendance comes back from teaching after every
                reload.
              </p>
            </header>
            {sessions.length === 0 ? (
              <EmptyState
                icon={<BookOpen aria-hidden="true" className="size-icon-lg" />}
                title="No sessions for this local date"
                description="Create a class with its first session to begin today's attendance list."
                action={
                  <Button onClick={openSetup}>
                    <Sparkles aria-hidden="true" className="size-icon-sm" />
                    Set up the first class
                  </Button>
                }
              />
            ) : (
              <div className="grid gap-4">
                {sessions.map((session) => (
                  <SessionAttendanceCard
                    key={session.session_id}
                    session={session}
                    timeZone={firstPage?.request_time_zone ?? 'UTC'}
                    pendingStudentId={
                      attendanceMutation.isPending &&
                      attendanceMutation.variables?.sessionId === session.session_id
                        ? attendanceMutation.variables.studentId
                        : undefined
                    }
                    savedMessage={
                      savedMessage?.sessionId === session.session_id
                        ? savedMessage.message
                        : undefined
                    }
                    error={
                      attendanceMutation.variables?.sessionId === session.session_id
                        ? attendanceMutation.error?.message
                        : undefined
                    }
                    onMark={(studentId, state) => {
                      setSavedMessage(undefined)
                      attendanceMutation.mutate({ sessionId: session.session_id, studentId, state })
                    }}
                  />
                ))}
                {homeQuery.hasNextPage && (
                  <Button
                    variant="secondary"
                    loading={homeQuery.isFetchingNextPage}
                    onClick={() => void homeQuery.fetchNextPage()}
                    className="w-full sm:w-fit"
                  >
                    Load more sessions
                  </Button>
                )}
              </div>
            )}
          </section>
        )}

        {firstPage && <div className="xl:hidden">{billingPanel}</div>}
      </PageEntrance>

      {firstPage && (
        <TeachingSetupSheet
          open={setupOpen}
          tutorId={firstPage.tutor.tutor_id}
          defaults={firstPage.setup_defaults}
          onOpenChange={setSetupOpen}
          onCreateClass={(input, key) => classMutation.mutateAsync({ input, key })}
          onCreateStudent={(input, key) => studentMutation.mutateAsync({ input, key })}
          onJoinRoster={(classId, input) => rosterMutation.mutateAsync({ classId, input })}
          onComplete={(localDate) => {
            const message =
              localDate === firstPage.local_date
                ? "Class setup complete. Today's session is ready for attendance."
                : `Class setup complete for ${formatLocalDate(localDate)}. It will appear on that local date.`
            setSetupMessage(message)
          }}
          returnFocusRef={setupReturnFocusRef}
        />
      )}
    </AppShell>
  )
}

function formatLocalDate(value: string) {
  return new Intl.DateTimeFormat('en', { dateStyle: 'full', timeZone: 'UTC' }).format(
    new Date(`${value}T00:00:00Z`),
  )
}
