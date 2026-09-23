import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Filter,
  History,
  Home,
  Plus,
  ReceiptText,
  RefreshCw,
  Repeat2,
  Users,
} from 'lucide-react'
import {
  type FormEvent,
  type MouseEvent as ReactMouseEvent,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { billingKeys } from '../api/billing'
import {
  cancelSession,
  endSchedule,
  moveSession,
  putSchedule,
  readSchedule,
  readTutor,
  restoreSession,
  type Schedule,
  type ScheduleSession,
} from '../api/teaching'
import { AccountPanel } from '../components/AccountPanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { AttendanceSheet } from '../components/AttendanceSheet'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FormField } from '../components/FormField'
import { PageEntrance } from '../components/PageEntrance'
import { ScheduleSessionCard } from '../components/ScheduleSessionCard'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader } from '../components/ui/card'
import { Checkbox } from '../components/ui/checkbox'
import { Input } from '../components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '../components/ui/sheet'
import { Skeleton } from '../components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs'

const scheduleRoute = getRouteApi('/schedule')
type CalendarView = 'day' | 'week' | 'month'

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
  { href: '/students', label: 'Students', icon: Users },
  { href: '/billing', label: 'Billing', icon: ReceiptText },
]

const weekdays = [
  { value: 1, label: 'Monday' },
  { value: 2, label: 'Tuesday' },
  { value: 3, label: 'Wednesday' },
  { value: 4, label: 'Thursday' },
  { value: 5, label: 'Friday' },
  { value: 6, label: 'Saturday' },
  { value: 7, label: 'Sunday' },
] as const

type SlotDraft = {
  weekday: number
  selected: boolean
  startTime: string
  endTime: string
}

type RetainedCommand = {
  key: string
  signature: string
}

/** Provides one tutor wide, Monday first calendar and every schedule action. */
export function SchedulePage() {
  const search = scheduleRoute.useSearch()
  const navigate = scheduleRoute.useNavigate()
  const queryClient = useQueryClient()
  const [isPhone, setIsPhone] = useState(false)
  const [selectedSession, setSelectedSession] = useState<ScheduleSession>()
  const [attendanceSessionID, setAttendanceSessionID] = useState<string>()
  const [scheduleClassID, setScheduleClassID] = useState<string>()
  const [announcement, setAnnouncement] = useState('')
  const [extraHistory, setExtraHistory] = useState<ScheduleSession[]>([])
  const [nextHistoryCursor, setNextHistoryCursor] = useState<string>()
  const sessionReturnFocusRef = useRef<HTMLButtonElement>(null)
  const scheduleReturnFocusRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    document.title = 'Schedule · Vermouth'
    const media = window.matchMedia('(max-width: 47.99rem)')
    const update = () => setIsPhone(media.matches)
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])

  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const tutor = tutorQuery.data
  const today = localDateInZone(new Date(), tutor?.timezone ?? 'UTC')
  const view: CalendarView = search.view ?? (isPhone ? 'day' : 'week')
  const visibleDate = search.date ?? today
  const selectedClassIDs = search.classes
  const visibleDates = useMemo(() => datesForView(visibleDate, view), [visibleDate, view])
  const from = visibleDates[0]
  const through = visibleDates[visibleDates.length - 1]
  const scheduleQuery = useQuery({
    queryKey: [
      'schedule',
      tutor?.tutor_id,
      tutor?.timezone,
      { from, through, classIds: selectedClassIDs, includeReplaced: true, historyCursor: null },
    ],
    queryFn: ({ signal }) =>
      readSchedule(
        { from, through, classIds: selectedClassIDs, includeReplaced: true, historyLimit: 50 },
        signal,
      ),
    enabled: tutor !== undefined,
  })

  const schedule = scheduleQuery.data
  useEffect(() => {
    setExtraHistory([])
    setNextHistoryCursor(schedule?.next_history_cursor ?? undefined)
  }, [schedule])
  const loadHistoryMutation = useMutation({
    mutationFn: async () => {
      if (!nextHistoryCursor) throw new Error('There is no later history page')
      return queryClient.fetchQuery({
        queryKey: [
          'schedule',
          tutor?.tutor_id,
          tutor?.timezone,
          {
            from,
            through,
            classIds: selectedClassIDs,
            includeReplaced: true,
            historyCursor: nextHistoryCursor,
          },
        ],
        queryFn: ({ signal }) =>
          readSchedule(
            {
              from,
              through,
              classIds: selectedClassIDs,
              includeReplaced: true,
              historyLimit: 50,
              historyCursor: nextHistoryCursor,
            },
            signal,
          ),
      })
    },
    onSuccess: (page) => {
      setExtraHistory((current) => [...current, ...page.replaced_history])
      setNextHistoryCursor(page.next_history_cursor ?? undefined)
      setAnnouncement(`${page.replaced_history.length} more replaced sessions loaded.`)
    },
  })
  const replacedHistory = [...(schedule?.replaced_history ?? []), ...extraHistory]
  const changeSearch = (next: Partial<{ view: CalendarView; date: string; classes: string[] }>) =>
    void navigate({ search: (current) => ({ ...current, ...next }), replace: true })
  const moveWindow = (direction: number) =>
    changeSearch({ date: addDates(visibleDate, direction * stepForView(view)) })
  const invalidateSchedule = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['schedule', tutor?.tutor_id] }),
      queryClient.invalidateQueries({ queryKey: billingKeys.all }),
    ])
  }
  const openSession = (session: ScheduleSession, trigger: HTMLButtonElement) => {
    sessionReturnFocusRef.current = trigger
    setSelectedSession(session)
  }
  const openSchedule = (classID: string, event: ReactMouseEvent<HTMLButtonElement>) => {
    scheduleReturnFocusRef.current = event.currentTarget
    setScheduleClassID(classID)
  }

  const contextualPanel = schedule ? (
    <div className="grid gap-7">
      <section aria-labelledby="schedule-month" className="grid gap-3">
        <div className="grid gap-1">
          <h2 id="schedule-month" className="text-base font-semibold">
            Visible date
          </h2>
          <p className="text-sm leading-relaxed text-muted-foreground">
            Every view and filter is kept in the page URL.
          </p>
        </div>
        <Input
          aria-label="Choose visible month"
          type="month"
          value={visibleDate.slice(0, 7)}
          onChange={(event) => changeSearch({ date: `${event.target.value}-01` })}
        />
      </section>
      <section aria-labelledby="schedule-classes" className="grid gap-3">
        <div className="flex items-center gap-2">
          <Filter aria-hidden="true" className="size-icon-sm text-muted-foreground" />
          <h2 id="schedule-classes" className="text-base font-semibold">
            Classes
          </h2>
        </div>
        <ul className="grid gap-2">
          {schedule.classes.map((item) => {
            const checked = selectedClassIDs.includes(item.class_id)
            return (
              <li
                key={item.class_id}
                data-class-color={item.color}
                className="grid gap-2 rounded-lg border border-class-border border-s-4 border-s-class-marker bg-class-surface p-2"
              >
                <label
                  htmlFor={`class-filter-${item.class_id}`}
                  className="flex min-h-11 cursor-pointer items-center gap-3 text-sm font-medium"
                >
                  <Checkbox
                    id={`class-filter-${item.class_id}`}
                    checked={checked}
                    onCheckedChange={(value) =>
                      changeSearch({
                        classes: value
                          ? [...selectedClassIDs, item.class_id].sort()
                          : selectedClassIDs.filter((valueID) => valueID !== item.class_id),
                      })
                    }
                  />
                  <span>{item.name}</span>
                </label>
                <Button
                  variant="quiet"
                  className="justify-start"
                  onClick={(event) => openSchedule(item.class_id, event)}
                >
                  <Repeat2 aria-hidden="true" className="size-icon-sm" />
                  Manage schedule
                </Button>
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  ) : undefined

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to schedule',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Change this device appearance.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={<AccountPanel />}
      contextualPanel={contextualPanel}
    >
      <PageEntrance className="mx-auto grid w-full max-w-7xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
        >
          <div className="grid max-w-3xl gap-3">
            <Badge variant="primary" className="w-fit">
              <CalendarDays aria-hidden="true" className="size-icon-sm" />
              Monday first calendar
            </Badge>
            <h1 className="text-3xl font-semibold text-balance">
              Your teaching time, at a glance.
            </h1>
            <p className="text-base leading-relaxed text-muted-foreground">
              See concrete sessions, keep exceptions visible, and change a series without losing its
              history.
            </p>
          </div>
          <Button onClick={(event) => openSchedule('', event)} disabled={!schedule?.classes.length}>
            <Plus aria-hidden="true" className="size-icon-sm" />
            Add schedule
          </Button>
        </header>

        <div className="flex flex-col gap-4 rounded-xl border border-border bg-surface p-3 shadow-field sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="icon"
              variant="quiet"
              aria-label="Previous period"
              onClick={() => moveWindow(-1)}
            >
              <ChevronLeft aria-hidden="true" className="size-icon-md" />
            </Button>
            <Button variant="secondary" onClick={() => changeSearch({ date: today })}>
              Today
            </Button>
            <Button
              size="icon"
              variant="quiet"
              aria-label="Next period"
              onClick={() => moveWindow(1)}
            >
              <ChevronRight aria-hidden="true" className="size-icon-md" />
            </Button>
            <p className="font-mono text-sm text-foreground">{formatRange(from, through)}</p>
          </div>
          <Tabs
            value={view}
            onValueChange={(value) => changeSearch({ view: value as CalendarView })}
          >
            <TabsList aria-label="Calendar view">
              <TabsTrigger value="day">Day</TabsTrigger>
              <TabsTrigger value="week">Week</TabsTrigger>
              <TabsTrigger value="month">Month</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>

        <p role="status" aria-live="polite" className="sr-only">
          {announcement}
        </p>
        <nav aria-label="Choose day" className="grid grid-cols-7 gap-1 md:hidden">
          {weekDates(visibleDate).map((date) => (
            <button
              key={date}
              type="button"
              aria-current={date === visibleDate ? 'date' : undefined}
              onClick={() => changeSearch({ date, view: 'day' })}
              className="grid min-h-11 place-items-center rounded-md border border-border bg-surface px-1 py-2 font-mono text-caption text-foreground outline-none transition-colors duration-base hover:border-primary aria-current:border-primary aria-current:bg-primary aria-current:text-primary-foreground focus-visible:ring-2 focus-visible:ring-focus motion-reduce:transition-none"
            >
              <span>{formatWeekday(date).slice(0, 1)}</span>
              <span>{date.slice(8)}</span>
            </button>
          ))}
        </nav>
        {tutorQuery.isPending || scheduleQuery.isPending ? (
          <ScheduleLoading />
        ) : tutorQuery.error || scheduleQuery.error ? (
          <ErrorState
            headingLevel="h2"
            title="The schedule could not be read"
            description={
              (tutorQuery.error ?? scheduleQuery.error)?.message ?? 'Try this view again.'
            }
            action={
              <Button variant="secondary" onClick={() => void scheduleQuery.refetch()}>
                <RefreshCw aria-hidden="true" className="size-icon-sm" />
                Try this view again
              </Button>
            }
          />
        ) : schedule ? (
          <Tabs value={view} className="grid gap-4">
            <TabsContent value="day">
              <DayView
                date={visibleDate}
                sessions={sessionsForDate(schedule, visibleDate)}
                onSelect={openSession}
              />
            </TabsContent>
            <TabsContent value="week">
              <WeekView dates={weekDates(visibleDate)} schedule={schedule} onSelect={openSession} />
            </TabsContent>
            <TabsContent value="month">
              <MonthView
                dates={monthDates(visibleDate)}
                schedule={schedule}
                visibleDate={visibleDate}
                onSelect={openSession}
              />
            </TabsContent>
          </Tabs>
        ) : null}

        {schedule && replacedHistory.length > 0 && (
          <section aria-labelledby="replaced-history" className="grid gap-4">
            <header className="flex items-center gap-2">
              <History aria-hidden="true" className="size-icon-md text-muted-foreground" />
              <div>
                <h2 id="replaced-history" className="text-xl font-semibold">
                  Replaced history
                </h2>
                <p className="text-sm text-muted-foreground">
                  Retained sessions from earlier rule versions, ordered by original date.
                </p>
              </div>
            </header>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {replacedHistory.map((session) => (
                <ScheduleSessionCard
                  key={session.session_id}
                  session={session}
                  onSelect={openSession}
                />
              ))}
            </div>
            {nextHistoryCursor && (
              <Button
                variant="secondary"
                className="w-fit"
                loading={loadHistoryMutation.isPending}
                onClick={() => loadHistoryMutation.mutate()}
              >
                Load more history
              </Button>
            )}
          </section>
        )}
      </PageEntrance>

      <SessionDetailsSheet
        session={selectedSession}
        open={selectedSession !== undefined}
        onOpenChange={(open) => !open && setSelectedSession(undefined)}
        returnFocusRef={sessionReturnFocusRef}
        onAttendance={(sessionID) => {
          setSelectedSession(undefined)
          setAttendanceSessionID(sessionID)
        }}
        onChanged={async (message) => {
          setAnnouncement(message)
          await invalidateSchedule()
          setSelectedSession(undefined)
        }}
      />
      {tutor && (
        <AttendanceSheet
          open={attendanceSessionID !== undefined}
          tutorId={tutor.tutor_id}
          sessionId={attendanceSessionID}
          onOpenChange={(open) => {
            if (!open) setAttendanceSessionID(undefined)
          }}
          returnFocusRef={sessionReturnFocusRef}
          onSaved={() => {
            setAnnouncement('Attendance saved for the whole roster.')
            void queryClient.invalidateQueries({ queryKey: ['home'] })
            void invalidateSchedule()
          }}
        />
      )}
      <ScheduleRuleSheet
        schedule={schedule}
        classID={scheduleClassID}
        onClassChange={setScheduleClassID}
        open={scheduleClassID !== undefined}
        onOpenChange={(open) => !open && setScheduleClassID(undefined)}
        returnFocusRef={scheduleReturnFocusRef}
        onChanged={async (message) => {
          setAnnouncement(message)
          await invalidateSchedule()
          setScheduleClassID(undefined)
        }}
      />
    </AppShell>
  )
}

function ScheduleLoading() {
  return (
    <Card role="status" aria-label="Loading schedule" aria-busy="true">
      <CardHeader className="gap-3">
        <Skeleton className="h-6 w-2/5" />
        <Skeleton className="h-4 w-3/4" />
      </CardHeader>
      <CardContent className="grid gap-3 md:grid-cols-3">
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </CardContent>
    </Card>
  )
}

function DayView({
  date,
  sessions,
  onSelect,
}: {
  date: string
  sessions: ScheduleSession[]
  onSelect: (session: ScheduleSession, trigger: HTMLButtonElement) => void
}) {
  return (
    <section aria-labelledby="day-heading" className="grid gap-4">
      <header className="grid gap-1">
        <h2 id="day-heading" className="text-xl font-semibold">
          {formatLongDate(date)}
        </h2>
        <p className="text-sm text-muted-foreground">Sessions are ordered by their current time.</p>
      </header>
      {sessions.length > 0 ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {sessions.map((session) => (
            <ScheduleSessionCard key={session.session_id} session={session} onSelect={onSelect} />
          ))}
        </div>
      ) : (
        <EmptyState
          icon={<CalendarDays aria-hidden="true" className="size-icon-lg" />}
          title="No sessions on this day"
          description="Choose another date or add a schedule for one of your classes."
        />
      )}
    </section>
  )
}

function WeekView({
  dates,
  schedule,
  onSelect,
}: {
  dates: string[]
  schedule: Schedule
  onSelect: (session: ScheduleSession, trigger: HTMLButtonElement) => void
}) {
  return (
    <section aria-labelledby="week-heading" className="grid gap-4">
      <h2 id="week-heading" className="text-xl font-semibold">
        Week schedule
      </h2>
      <div className="grid gap-3 md:grid-cols-7 md:gap-0 md:overflow-hidden md:rounded-xl md:border md:border-border md:bg-surface md:shadow-field">
        {dates.map((date) => {
          const sessions = sessionsForDate(schedule, date)
          return (
            <section
              key={date}
              aria-labelledby={`day-${date}`}
              className="min-w-0 rounded-xl border border-border bg-surface p-3 md:rounded-none md:border-0 md:border-e md:p-2 md:last:border-e-0"
            >
              <header className="mb-3 grid gap-1 border-b border-border pb-2 text-center">
                <h3 id={`day-${date}`} className="text-sm font-semibold">
                  {formatWeekday(date)}
                </h3>
                <time dateTime={date} className="font-mono text-caption text-muted-foreground">
                  {date.slice(8)}
                </time>
              </header>
              <div className="grid gap-2">
                {sessions.map((session) => (
                  <ScheduleSessionCard
                    key={session.session_id}
                    session={session}
                    onSelect={onSelect}
                  />
                ))}
                {sessions.length === 0 && (
                  <p className="py-4 text-center text-caption text-muted-foreground">No session</p>
                )}
              </div>
            </section>
          )
        })}
      </div>
    </section>
  )
}

function MonthView({
  dates,
  schedule,
  visibleDate,
  onSelect,
}: {
  dates: string[]
  schedule: Schedule
  visibleDate: string
  onSelect: (session: ScheduleSession, trigger: HTMLButtonElement) => void
}) {
  return (
    <section aria-labelledby="month-heading" className="grid gap-4">
      <h2 id="month-heading" className="text-xl font-semibold">
        {formatMonth(visibleDate)}
      </h2>
      <div className="grid grid-cols-7 overflow-hidden rounded-xl border border-border bg-surface shadow-field">
        {weekdays.map((weekday) => (
          <p
            key={weekday.value}
            className="border-b border-e border-border px-1 py-2 text-center text-caption font-semibold last:border-e-0"
          >
            {weekday.label.slice(0, 3)}
          </p>
        ))}
        {dates.map((date) => {
          const sessions = sessionsForDate(schedule, date)
          const currentMonth = date.slice(0, 7) === visibleDate.slice(0, 7)
          return (
            <section
              key={date}
              aria-label={formatLongDate(date)}
              className="min-h-24 border-e border-b border-border p-1 last:border-e-0 sm:min-h-32 sm:p-2"
            >
              <time
                dateTime={date}
                className={`font-mono text-caption ${currentMonth ? 'text-foreground' : 'text-muted-foreground'}`}
              >
                {date.slice(8)}
              </time>
              <div className="mt-1 grid gap-1">
                {sessions.slice(0, 3).map((session) => (
                  <button
                    key={session.session_id}
                    type="button"
                    onClick={(event) => onSelect(session, event.currentTarget)}
                    data-class-color={session.class_color}
                    className="min-h-11 rounded-md border border-class-border border-s-4 border-s-class-marker bg-class-surface p-1 text-start text-caption font-medium outline-none focus-visible:ring-2 focus-visible:ring-focus"
                  >
                    <span className="block font-mono">{session.display_start}</span>
                    <span className="block truncate">{session.class_name}</span>
                  </button>
                ))}
                {sessions.length > 3 && (
                  <p className="text-caption text-muted-foreground">{sessions.length - 3} more</p>
                )}
              </div>
            </section>
          )
        })}
      </div>
    </section>
  )
}

function SessionDetailsSheet({
  session,
  open,
  onOpenChange,
  onChanged,
  onAttendance,
  returnFocusRef,
}: {
  session?: ScheduleSession
  open: boolean
  onOpenChange: (open: boolean) => void
  onChanged: (message: string) => Promise<void>
  onAttendance: (sessionID: string) => void
  returnFocusRef: RefObject<HTMLButtonElement | null>
}) {
  const [date, setDate] = useState('')
  const [startTime, setStartTime] = useState('')
  const [endTime, setEndTime] = useState('')
  const commandRef = useRef<RetainedCommand | undefined>(undefined)
  useEffect(() => {
    if (!session) return
    setDate(session.display_date)
    setStartTime(session.display_start)
    setEndTime(session.display_end)
  }, [session])
  const mutation = useMutation({
    mutationFn: async (action: 'move' | 'cancel' | 'restore') => {
      if (!session) throw new Error('Choose a session first')
      if (action === 'move') {
        const input = {
          expected_version: session.version,
          local_date: date,
          start_time: startTime,
          end_time: endTime,
        }
        return moveSession(
          session.session_id,
          input,
          retainedCommandKey(commandRef, { action, input }),
        )
      }
      const input = { expected_version: session.version }
      return action === 'cancel'
        ? cancelSession(
            session.session_id,
            input,
            retainedCommandKey(commandRef, { action, input }),
          )
        : restoreSession(
            session.session_id,
            input,
            retainedCommandKey(commandRef, { action, input }),
          )
    },
    onSuccess: async (_result, action) => {
      commandRef.current = undefined
      await onChanged(
        action === 'move'
          ? `Session moved to ${date} at ${startTime}.`
          : action === 'cancel'
            ? 'Session cancelled.'
            : 'Session restored.',
      )
    },
  })
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="end"
        closeLabel="Close session details"
        className="content-start"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          restoreSheetFocus(returnFocusRef)
        }}
      >
        <SheetHeader>
          <SheetTitle>{session?.class_name ?? 'Session details'}</SheetTitle>
          <SheetDescription>
            Current time, rule origin, and the actions valid for this session.
          </SheetDescription>
        </SheetHeader>
        {session && (
          <div className="grid gap-6">
            <dl className="grid gap-3 rounded-xl border border-border bg-muted p-4 text-sm">
              <div>
                <dt className="text-muted-foreground">Current time</dt>
                <dd className="font-mono">
                  {session.display_date} · {session.display_start} to {session.display_end}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">State and version</dt>
                <dd>
                  {session.state}
                  {session.moved_at ? ' · moved' : ''} · {session.version}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Rule origin</dt>
                <dd className="break-all">
                  {session.schedule_rule_id ?? 'Standalone session'} · {session.origin_local_date}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Source zone and billing date</dt>
                <dd>
                  {session.source_time_zone ?? 'Current tutor zone'} · {session.local_date}
                </dd>
              </div>
            </dl>
            {session.state === 'active' && (
              <form
                className="grid gap-4"
                onSubmit={(event) => {
                  event.preventDefault()
                  mutation.mutate('move')
                }}
              >
                <h3 className="text-lg font-semibold">Move this session</h3>
                <FormField
                  controlId="move-date"
                  label="New date"
                  control={(props) => (
                    <Input
                      {...props}
                      type="date"
                      value={date}
                      onChange={(event) => setDate(event.target.value)}
                      required
                    />
                  )}
                />
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField
                    controlId="move-start"
                    label="Start time"
                    control={(props) => (
                      <Input
                        {...props}
                        type="time"
                        value={startTime}
                        onChange={(event) => setStartTime(event.target.value)}
                        required
                      />
                    )}
                  />
                  <FormField
                    controlId="move-end"
                    label="End time"
                    control={(props) => (
                      <Input
                        {...props}
                        type="time"
                        value={endTime}
                        onChange={(event) => setEndTime(event.target.value)}
                        required
                      />
                    )}
                  />
                </div>
                <Button type="submit" loading={mutation.isPending}>
                  Move session
                </Button>
              </form>
            )}
            {mutation.error && (
              <p
                role="alert"
                className="rounded-lg bg-destructive-surface p-3 text-sm text-destructive-foreground"
              >
                {mutation.error.message} Refresh the calendar and review the current details.
              </p>
            )}
          </div>
        )}
        <SheetFooter>
          {session?.state === 'active' && (
            <Button variant="secondary" onClick={() => onAttendance(session.session_id)}>
              Mark attendance
            </Button>
          )}
          {session?.state === 'active' && (
            <Button
              variant="destructive"
              onClick={() => mutation.mutate('cancel')}
              loading={mutation.isPending}
            >
              Cancel session
            </Button>
          )}
          {session?.state === 'cancelled' && (
            <Button onClick={() => mutation.mutate('restore')} loading={mutation.isPending}>
              Restore session
            </Button>
          )}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function ScheduleRuleSheet({
  schedule,
  classID,
  onClassChange,
  open,
  onOpenChange,
  onChanged,
  returnFocusRef,
}: {
  schedule?: Schedule
  classID?: string
  onClassChange: (classID: string) => void
  open: boolean
  onOpenChange: (open: boolean) => void
  onChanged: (message: string) => Promise<void>
  returnFocusRef: RefObject<HTMLButtonElement | null>
}) {
  const selectedClass = schedule?.classes.find((item) => item.class_id === classID)
  const latestRule = schedule?.rules
    .filter((rule) => rule.class_id === classID)
    .sort((left, right) => right.revision - left.revision)[0]
  const [effectiveFrom, setEffectiveFrom] = useState('')
  const [validThrough, setValidThrough] = useState('')
  const [lastDate, setLastDate] = useState('')
  const [slots, setSlots] = useState<SlotDraft[]>(defaultSlots())
  const saveCommandRef = useRef<RetainedCommand | undefined>(undefined)
  const endCommandRef = useRef<RetainedCommand | undefined>(undefined)
  useEffect(() => {
    if (!open) return
    const today = localDateInZone(new Date(), schedule?.request_time_zone ?? 'UTC')
    setEffectiveFrom(latestRule?.valid_from ?? today)
    setValidThrough(latestRule?.valid_through ?? addDates(today, 90))
    setLastDate(latestRule?.valid_through ?? today)
    const initialSlots = defaultSlots()
    setSlots(
      latestRule
        ? initialSlots.map((slot) => {
            const current = latestRule?.slots.find((value) => value.weekday === slot.weekday)
            return current
              ? {
                  ...slot,
                  selected: true,
                  startTime: current.start_time,
                  endTime: current.end_time,
                }
              : { ...slot, selected: false }
          })
        : initialSlots,
    )
  }, [latestRule, open, schedule?.request_time_zone])
  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!selectedClass) throw new Error('Choose a class first')
      const selectedSlots = slots.filter((slot) => slot.selected)
      if (selectedSlots.length === 0) throw new Error('Choose at least one weekday')
      const input = {
        expected_revision: selectedClass.schedule_revision,
        effective_from: effectiveFrom,
        valid_through: validThrough,
        slots: selectedSlots.map((slot) => ({
          weekday: slot.weekday,
          start_time: slot.startTime,
          end_time: slot.endTime,
        })),
      }
      return putSchedule(selectedClass.class_id, input, retainedCommandKey(saveCommandRef, input))
    },
    onSuccess: async (result) => {
      saveCommandRef.current = undefined
      await onChanged(
        `Schedule saved. ${result.created_count} created, ${result.adopted_count} adopted, ${result.preserved_count} exceptions preserved, and ${result.superseded_count} sessions replaced.`,
      )
    },
  })
  const endMutation = useMutation({
    mutationFn: async () => {
      if (!selectedClass) throw new Error('Choose a class first')
      const input = { expected_revision: selectedClass.schedule_revision, last_date: lastDate }
      return endSchedule(selectedClass.class_id, input, retainedCommandKey(endCommandRef, input))
    },
    onSuccess: async (result) => {
      endCommandRef.current = undefined
      await onChanged(
        `Schedule ended. ${result.preserved_count} exceptions preserved and ${result.superseded_count} sessions replaced.`,
      )
    },
  })
  const error = saveMutation.error ?? endMutation.error
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="end"
        closeLabel="Close schedule form"
        className="content-start sm:w-[min(34rem,92vw)]"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          restoreSheetFocus(returnFocusRef)
        }}
      >
        <SheetHeader>
          <SheetTitle>
            {selectedClass ? `Schedule for ${selectedClass.name}` : 'Add schedule'}
          </SheetTitle>
          <SheetDescription>
            Each weekday keeps its own time. Replacing or ending a rule preserves moved and tutor
            cancelled exceptions.
          </SheetDescription>
        </SheetHeader>
        {!classID && schedule && (
          <FormField
            controlId="schedule-class"
            label="Class"
            hint="Choose the class that will own this weekly rule."
            control={(props) => (
              <Select value={classID || undefined} onValueChange={onClassChange}>
                <SelectTrigger {...props}>
                  <SelectValue placeholder="Choose a class" />
                </SelectTrigger>
                <SelectContent>
                  {schedule.classes.map((item) => (
                    <SelectItem key={item.class_id} value={item.class_id}>
                      {item.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
        )}
        <form
          className="grid gap-6"
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            saveMutation.mutate()
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              controlId="schedule-effective"
              label={latestRule ? 'Change from' : 'Start date'}
              control={(props) => (
                <Input
                  {...props}
                  type="date"
                  value={effectiveFrom}
                  onChange={(event) => setEffectiveFrom(event.target.value)}
                  required
                />
              )}
            />
            <FormField
              controlId="schedule-through"
              label="Inclusive end date"
              control={(props) => (
                <Input
                  {...props}
                  type="date"
                  value={validThrough}
                  onChange={(event) => setValidThrough(event.target.value)}
                  required
                />
              )}
            />
          </div>
          <fieldset className="grid gap-3">
            <legend className="text-sm font-semibold">Weekly times</legend>
            {slots.map((slot, index) => (
              <div
                key={slot.weekday}
                className="grid gap-3 rounded-lg border border-border p-3 sm:grid-cols-[auto_1fr_1fr] sm:items-end"
              >
                <label
                  htmlFor={`slot-${slot.weekday}-selected`}
                  className="flex min-h-11 items-center gap-3 text-sm font-medium"
                >
                  <Checkbox
                    id={`slot-${slot.weekday}-selected`}
                    checked={slot.selected}
                    onCheckedChange={(checked) =>
                      setSlots((current) =>
                        current.map((value, valueIndex) =>
                          valueIndex === index ? { ...value, selected: Boolean(checked) } : value,
                        ),
                      )
                    }
                  />
                  {weekdays[index].label}
                </label>
                <FormField
                  controlId={`slot-${slot.weekday}-start`}
                  label="Starts"
                  control={(props) => (
                    <Input
                      {...props}
                      type="time"
                      value={slot.startTime}
                      disabled={!slot.selected}
                      onChange={(event) =>
                        setSlots((current) =>
                          current.map((value, valueIndex) =>
                            valueIndex === index
                              ? { ...value, startTime: event.target.value }
                              : value,
                          ),
                        )
                      }
                      required={slot.selected}
                    />
                  )}
                />
                <FormField
                  controlId={`slot-${slot.weekday}-end`}
                  label="Ends"
                  control={(props) => (
                    <Input
                      {...props}
                      type="time"
                      value={slot.endTime}
                      disabled={!slot.selected}
                      onChange={(event) =>
                        setSlots((current) =>
                          current.map((value, valueIndex) =>
                            valueIndex === index
                              ? { ...value, endTime: event.target.value }
                              : value,
                          ),
                        )
                      }
                      required={slot.selected}
                    />
                  )}
                />
              </div>
            ))}
          </fieldset>
          <p className="rounded-lg bg-muted p-3 text-sm text-muted-foreground">
            Estimated candidates:{' '}
            <strong className="font-mono text-foreground">
              {estimateCandidates(effectiveFrom, validThrough, slots)}
            </strong>
            . The saved result is authoritative after time zone and exception checks.
          </p>
          {error && (
            <p
              role="alert"
              className="rounded-lg bg-destructive-surface p-3 text-sm text-destructive-foreground"
            >
              {error.message} Keep this draft, refresh the calendar, then try again with the current
              revision.
            </p>
          )}
          <Button type="submit" loading={saveMutation.isPending} disabled={!selectedClass}>
            Save schedule
          </Button>
        </form>
        {latestRule && (
          <section
            aria-labelledby="end-schedule-heading"
            className="grid gap-3 border-t border-border pt-5"
          >
            <h3 id="end-schedule-heading" className="text-lg font-semibold">
              End this schedule early
            </h3>
            <FormField
              controlId="schedule-last-date"
              label="Last teaching date"
              hint="Moved and tutor cancelled exceptions remain in history."
              control={(props) => (
                <Input
                  {...props}
                  type="date"
                  value={lastDate}
                  onChange={(event) => setLastDate(event.target.value)}
                  required
                />
              )}
            />
            <Button
              variant="destructive"
              onClick={() => endMutation.mutate()}
              loading={endMutation.isPending}
            >
              End schedule
            </Button>
          </section>
        )}
      </SheetContent>
    </Sheet>
  )
}

function restoreSheetFocus(returnFocusRef: RefObject<HTMLButtonElement | null>) {
  const trigger = returnFocusRef.current
  if (trigger?.isConnected) {
    trigger.focus()
    return
  }
  document.getElementById('main-content')?.focus()
}

function retainedCommandKey(
  commandRef: RefObject<RetainedCommand | undefined>,
  input: unknown,
): string {
  const signature = JSON.stringify(input)
  if (commandRef.current?.signature !== signature) {
    commandRef.current = { key: crypto.randomUUID(), signature }
  }
  return commandRef.current.key
}

function defaultSlots(): SlotDraft[] {
  return weekdays.map((weekday, index) => ({
    weekday: weekday.value,
    selected: index === 0,
    startTime: '17:30',
    endTime: '19:00',
  }))
}

function estimateCandidates(from: string, through: string, slots: SlotDraft[]): number {
  if (!from || !through || through < from) return 0
  const selected = new Set(slots.filter((slot) => slot.selected).map((slot) => slot.weekday))
  let count = 0
  for (let date = from; date <= through; date = addDates(date, 1)) {
    const day = new Date(`${date}T00:00:00Z`).getUTCDay()
    if (selected.has(day === 0 ? 7 : day)) count += 1
  }
  return count
}

function sessionsForDate(schedule: Schedule, date: string): ScheduleSession[] {
  return schedule.sessions.filter((session) => session.display_date === date)
}

function localDateInZone(value: Date, timeZone: string): string {
  const parts = new Intl.DateTimeFormat('en', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(value)
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((item) => item.type === type)?.value
  return `${part('year')}-${part('month')}-${part('day')}`
}

function addDates(value: string, days: number): string {
  const date = new Date(`${value}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}

function weekDates(value: string): string[] {
  const day = new Date(`${value}T00:00:00Z`).getUTCDay()
  const monday = addDates(value, day === 0 ? -6 : 1 - day)
  return Array.from({ length: 7 }, (_, index) => addDates(monday, index))
}

function monthDates(value: string): string[] {
  const start = weekDates(`${value.slice(0, 7)}-01`)[0]
  return Array.from({ length: 42 }, (_, index) => addDates(start, index))
}

function datesForView(value: string, view: CalendarView): string[] {
  if (view === 'day') return [value]
  if (view === 'week') return weekDates(value)
  return monthDates(value)
}

function stepForView(view: CalendarView): number {
  if (view === 'day') return 1
  if (view === 'week') return 7
  return 28
}

function formatWeekday(value: string): string {
  return new Intl.DateTimeFormat('en', { weekday: 'short', timeZone: 'UTC' }).format(
    new Date(`${value}T00:00:00Z`),
  )
}

function formatLongDate(value: string): string {
  return new Intl.DateTimeFormat('en', { dateStyle: 'full', timeZone: 'UTC' }).format(
    new Date(`${value}T00:00:00Z`),
  )
}

function formatMonth(value: string): string {
  return new Intl.DateTimeFormat('en', { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(
    new Date(`${value.slice(0, 7)}-01T00:00:00Z`),
  )
}

function formatRange(from: string, through: string): string {
  const format = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC' })
  if (from === through) return format.format(new Date(`${from}T00:00:00Z`))
  return `${format.format(new Date(`${from}T00:00:00Z`))} to ${format.format(new Date(`${through}T00:00:00Z`))}`
}
