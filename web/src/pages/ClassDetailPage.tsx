import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { CalendarDays, Home, ListChecks, Users } from 'lucide-react'
import { type MouseEvent as ReactMouseEvent, useEffect, useRef, useState } from 'react'

import {
  type ClassRoster,
  classRosterKeys,
  type RosterStudent,
  readClassRoster,
  readTutor,
  studentKeys,
} from '../api/teaching'
import { AccountPanel } from '../components/AccountPanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FormField } from '../components/FormField'
import { PageEntrance } from '../components/PageEntrance'
import { ResponsiveTable } from '../components/ResponsiveTable'
import { RosterManagementSheet } from '../components/RosterManagementSheet'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Skeleton } from '../components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'

const classRoute = getRouteApi('/classes/$classId')

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
  { href: '/students', label: 'Students', icon: Users },
]

/** Shows one dated class roster and stages one atomic membership delta. */
export function ClassDetailPage() {
  const { classId } = classRoute.useParams()
  const search = classRoute.useSearch()
  const navigate = classRoute.useNavigate()
  const queryClient = useQueryClient()
  const [manageOpen, setManageOpen] = useState(false)
  const [announcement, setAnnouncement] = useState('')
  const manageReturnFocusRef = useRef<HTMLButtonElement>(null)

  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const tutor = tutorQuery.data
  const today = tutor ? localDateInZone(new Date(), tutor.timezone) : undefined
  const rosterQuery = useQuery({
    queryKey: classRosterKeys.detail(tutor?.tutor_id ?? '', classId, search.date),
    queryFn: ({ signal }) => readClassRoster(classId, search.date, signal),
    enabled: tutor !== undefined,
  })
  const roster = rosterQuery.data

  useEffect(() => {
    document.title = roster ? `${roster.class.name} roster · Vermouth` : 'Class roster · Vermouth'
  }, [roster])

  function openManage(event: ReactMouseEvent<HTMLButtonElement>) {
    manageReturnFocusRef.current = event.currentTarget
    setManageOpen(true)
  }

  const loading = tutorQuery.isPending || rosterQuery.isPending
  const error = tutorQuery.error ?? rosterQuery.error

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to class roster',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Change this device appearance.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={<AccountPanel />}
    >
      <PageEntrance className="mx-auto grid w-full max-w-6xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        {loading ? (
          <Card role="status" aria-label="Loading class roster" aria-busy="true">
            <CardHeader className="gap-3">
              <Skeleton className="h-7 w-2/5" />
              <Skeleton className="h-4 w-3/5" />
            </CardHeader>
            <CardContent className="grid gap-3">
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
            </CardContent>
          </Card>
        ) : error || !roster || !tutor ? (
          <ErrorState
            headingLevel="h2"
            title="This class roster could not be read"
            description={error?.message ?? 'No active class matched this address.'}
            action={
              <Button asChild variant="secondary">
                <Link to="/schedule" search={{ classes: [] }}>
                  Return to schedule
                </Link>
              </Button>
            }
          />
        ) : (
          <>
            <header
              data-entrance-item
              data-class-color={roster.class.color}
              className="grid gap-5 rounded-xl border border-class-border border-s-4 border-s-class-marker bg-class-surface p-5 shadow-field sm:p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
            >
              <div className="grid gap-3">
                <Badge variant="primary" className="w-fit">
                  <ListChecks aria-hidden="true" className="size-icon-sm" />
                  Dated class roster
                </Badge>
                <div className="grid gap-1">
                  <h1 className="text-3xl font-semibold text-balance">{roster.class.name}</h1>
                  <p className="text-base leading-relaxed text-muted-foreground">
                    {roster.students.length === 1
                      ? '1 student covered on this date.'
                      : `${roster.students.length} students covered on this date.`}
                  </p>
                </div>
              </div>
              <Button size="large" className="w-full lg:w-auto" onClick={openManage}>
                Manage roster
              </Button>
            </header>

            <div
              role="status"
              aria-live="polite"
              className="text-sm font-medium text-success-foreground"
            >
              {announcement}
            </div>

            <section data-entrance-item aria-labelledby="roster-heading" className="grid gap-4">
              <div className="grid gap-4 rounded-xl border border-border bg-surface p-4 shadow-field sm:grid-cols-[minmax(0,1fr)_15rem] sm:items-end sm:p-5">
                <div className="grid gap-1">
                  <h2 id="roster-heading" className="text-xl font-semibold">
                    Roster on {formatDate(roster.resolved_date)}
                  </h2>
                  <p className="text-sm leading-relaxed text-muted-foreground">
                    Phone is visible only for an active student on today’s roster.
                  </p>
                </div>
                <FormField
                  controlId="roster-date"
                  label="Roster date"
                  control={(accessibility) => (
                    <Input
                      {...accessibility}
                      type="date"
                      max={today}
                      value={roster.resolved_date}
                      onChange={(event) =>
                        void navigate({
                          to: '/classes/$classId',
                          params: { classId },
                          search: { date: event.target.value },
                        })
                      }
                    />
                  )}
                />
              </div>

              <ResponsiveTable
                rows={roster.students}
                getRowKey={(student) => student.student_id}
                emptyState={
                  <EmptyState
                    icon={<Users aria-hidden="true" className="size-icon-lg" />}
                    title="No students are covered on this date"
                    description="Choose Manage roster to add active students with a dated change."
                    action={<Button onClick={openManage}>Manage roster</Button>}
                  />
                }
                renderCard={(student) => <RosterStudentCard student={student} />}
                renderTable={(students) => <RosterTable students={students} />}
              />
            </section>

            <RosterManagementSheet
              open={manageOpen}
              tutorId={tutor.tutor_id}
              roster={roster}
              onOpenChange={setManageOpen}
              returnFocusRef={manageReturnFocusRef}
              onConflict={async () => {
                await rosterQuery.refetch()
              }}
              onSaved={(saved: ClassRoster) => {
                queryClient.setQueryData(
                  classRosterKeys.detail(tutor.tutor_id, classId, search.date),
                  saved,
                )
                void queryClient.invalidateQueries({ queryKey: studentKeys.all })
                void queryClient.invalidateQueries({ queryKey: ['home'] })
                void queryClient.invalidateQueries({ queryKey: ['schedule'] })
                setAnnouncement(`Roster saved for ${formatDate(saved.resolved_date)}.`)
              }}
            />
          </>
        )}
      </PageEntrance>
    </AppShell>
  )
}

function RosterStudentCard({ student }: { student: RosterStudent }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle>{student.name}</CardTitle>
          {student.archived && <Badge variant="warning">Archived</Badge>}
        </div>
        <p className="text-sm text-muted-foreground">{student.phone ?? 'Phone hidden'}</p>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm text-muted-foreground">
        <p>
          {formatDate(student.effective_from)} to{' '}
          {student.effective_to ? formatDate(student.effective_to) : 'present'}
        </p>
        <Button asChild variant="secondary" className="w-fit">
          <Link to="/students/$studentId" params={{ studentId: student.student_id }}>
            Open student
          </Link>
        </Button>
      </CardContent>
    </Card>
  )
}

function RosterTable({ students }: { students: readonly RosterStudent[] }) {
  return (
    <Card className="overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead scope="col">Student</TableHead>
            <TableHead scope="col">Phone</TableHead>
            <TableHead scope="col">Membership</TableHead>
            <TableHead scope="col">
              <span className="sr-only">Open record</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {students.map((student) => (
            <TableRow key={student.student_id}>
              <TableCell className="font-medium">
                <span className="flex flex-wrap items-center gap-2">
                  {student.name}
                  {student.archived && <Badge variant="warning">Archived</Badge>}
                </span>
              </TableCell>
              <TableCell>{student.phone ?? 'Hidden'}</TableCell>
              <TableCell>
                {formatDate(student.effective_from)} to{' '}
                {student.effective_to ? formatDate(student.effective_to) : 'present'}
              </TableCell>
              <TableCell className="text-end">
                <Button asChild variant="quiet">
                  <Link to="/students/$studentId" params={{ studentId: student.student_id }}>
                    Open
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  )
}

function localDateInZone(value: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('en', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(value)
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((candidate) => candidate.type === type)?.value ?? ''
  return `${part('year')}-${part('month')}-${part('day')}`
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeZone: 'UTC' }).format(
    new Date(`${value}T00:00:00Z`),
  )
}
