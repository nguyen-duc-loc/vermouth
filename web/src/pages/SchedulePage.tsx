import { useQuery } from '@tanstack/react-query'
import { CalendarDays, Home, RefreshCw } from 'lucide-react'
import { useEffect, useMemo } from 'react'

import { readSchedule, readTutor } from '../api/teaching'
import { AppearancePanel, type AppearancePanelText } from '../components/AppearancePanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PageEntrance } from '../components/PageEntrance'
import { ScheduleSessionCard } from '../components/ScheduleSessionCard'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader } from '../components/ui/card'
import { Skeleton } from '../components/ui/skeleton'

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
]

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

/** Opens the tutor wide calendar on a Monday first weekly surface. */
export function SchedulePage() {
  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const tutor = tutorQuery.data
  const visibleWeek = useMemo(
    () => weekDates(localDateInZone(new Date(), tutor?.timezone ?? 'UTC')),
    [tutor?.timezone],
  )
  const from = visibleWeek[0]
  const through = visibleWeek[6]
  const scheduleQuery = useQuery({
    queryKey: [
      'schedule',
      tutor?.tutor_id,
      tutor?.timezone,
      { from, through, classIds: [], includeReplaced: false, historyCursor: null },
    ],
    queryFn: ({ signal }) => readSchedule({ from, through, classIds: [] }, signal),
    enabled: tutor !== undefined,
  })

  useEffect(() => {
    document.title = 'Schedule · Vermouth'
  }, [])

  const schedule = scheduleQuery.data
  const currentDate = localDateInZone(new Date(), tutor?.timezone ?? 'UTC')
  const currentSessions = schedule?.sessions.filter(
    (session) => session.display_date === currentDate,
  )
  const contextualPanel = schedule ? (
    <section aria-labelledby="schedule-classes" className="grid gap-4">
      <header className="grid gap-1">
        <h2 id="schedule-classes" className="text-base font-semibold">
          Classes
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          Class colors mark the same teaching identity across every view.
        </p>
      </header>
      <ul className="grid gap-2">
        {schedule.classes.map((item) => (
          <li
            key={item.class_id}
            data-class-color={item.color}
            className="rounded-lg border border-class-border border-s-4 border-s-class-marker bg-class-surface px-3 py-3 text-sm font-medium"
          >
            {item.name}
          </li>
        ))}
      </ul>
    </section>
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
      appearancePanel={<AppearancePanel text={appearanceText} />}
      contextualPanel={contextualPanel}
    >
      <PageEntrance className="mx-auto grid w-full max-w-7xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
        >
          <div className="grid max-w-3xl gap-3">
            <Badge variant="primary" className="w-fit">
              <CalendarDays aria-hidden="true" className="size-icon-sm" />
              Monday first week
            </Badge>
            <h1 className="text-3xl font-semibold text-balance">
              Your teaching week, in one view.
            </h1>
            <p className="text-base leading-relaxed text-muted-foreground">
              Concrete sessions stay in chronological order, with the class name beside every color.
            </p>
          </div>
          <p className="rounded-lg border border-border bg-surface px-4 py-3 font-mono text-sm text-foreground">
            {formatRange(from, through)}
          </p>
        </header>

        {tutorQuery.isPending || scheduleQuery.isPending ? (
          <Card role="status" aria-label="Loading weekly schedule" aria-busy="true">
            <CardHeader className="gap-3">
              <Skeleton className="h-6 w-2/5" />
              <Skeleton className="h-4 w-3/4" />
            </CardHeader>
            <CardContent className="grid gap-3 md:grid-cols-3">
              <Skeleton className="h-32 w-full" />
              <Skeleton className="h-32 w-full" />
              <Skeleton className="h-32 w-full" />
            </CardContent>
          </Card>
        ) : tutorQuery.error || scheduleQuery.error ? (
          <ErrorState
            headingLevel="h2"
            title="The schedule could not be read"
            description={
              (tutorQuery.error ?? scheduleQuery.error)?.message ?? 'Try the week again.'
            }
            action={
              <Button variant="secondary" onClick={() => void scheduleQuery.refetch()}>
                <RefreshCw aria-hidden="true" className="size-icon-sm" />
                Try the week again
              </Button>
            }
          />
        ) : schedule && schedule.sessions.length === 0 ? (
          <EmptyState
            icon={<CalendarDays aria-hidden="true" className="size-icon-lg" />}
            title="No sessions in this week"
            description="Create a weekly schedule or move to a week that already has teaching sessions."
          />
        ) : schedule ? (
          <>
            <section aria-labelledby="phone-agenda" className="grid gap-4 md:hidden">
              <header className="grid gap-1">
                <h2 id="phone-agenda" className="text-xl font-semibold">
                  Today
                </h2>
                <p className="text-sm text-muted-foreground">{formatLongDate(currentDate)}</p>
              </header>
              {currentSessions && currentSessions.length > 0 ? (
                <div className="grid gap-3">
                  {currentSessions.map((session) => (
                    <ScheduleSessionCard key={session.session_id} session={session} />
                  ))}
                </div>
              ) : (
                <EmptyState
                  title="No sessions today"
                  description="The rest of this week remains available on a wider screen."
                />
              )}
            </section>

            <section aria-labelledby="week-heading" className="hidden gap-4 md:grid">
              <h2 id="week-heading" className="text-xl font-semibold">
                Week schedule
              </h2>
              <div className="grid grid-cols-7 overflow-hidden rounded-xl border border-border bg-surface shadow-field">
                {visibleWeek.map((date) => {
                  const sessions = schedule.sessions.filter(
                    (session) => session.display_date === date,
                  )
                  return (
                    <section
                      key={date}
                      aria-labelledby={`day-${date}`}
                      className="min-w-0 border-e border-border p-2 last:border-e-0"
                    >
                      <header className="mb-3 grid gap-1 border-b border-border pb-2 text-center">
                        <h3 id={`day-${date}`} className="text-sm font-semibold">
                          {formatWeekday(date)}
                        </h3>
                        <time
                          dateTime={date}
                          className="font-mono text-caption text-muted-foreground"
                        >
                          {date.slice(8)}
                        </time>
                      </header>
                      <div className="grid gap-2">
                        {sessions.map((session) => (
                          <ScheduleSessionCard key={session.session_id} session={session} />
                        ))}
                        {sessions.length === 0 && (
                          <p className="py-4 text-center text-caption text-muted-foreground">
                            No session
                          </p>
                        )}
                      </div>
                    </section>
                  )
                })}
              </div>
            </section>
          </>
        ) : null}
      </PageEntrance>
    </AppShell>
  )
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
  const mondayOffset = day === 0 ? -6 : 1 - day
  const monday = addDates(value, mondayOffset)
  return Array.from({ length: 7 }, (_, index) => addDates(monday, index))
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

function formatRange(from: string, through: string): string {
  const format = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC' })
  return `${format.format(new Date(`${from}T00:00:00Z`))} to ${format.format(new Date(`${through}T00:00:00Z`))}`
}
