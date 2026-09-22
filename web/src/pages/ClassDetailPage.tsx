import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { CalendarDays, Home, ListChecks, ReceiptText, Users } from 'lucide-react'
import { type MouseEvent as ReactMouseEvent, useEffect, useRef, useState } from 'react'

import {
  billingKeys,
  type ClassRates,
  classRateKeys,
  putClassRate,
  readClassRates,
} from '../api/billing'
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '../components/ui/dialog'
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
  { href: '/billing', label: 'Billing', icon: ReceiptText },
]

/** Shows one dated class roster and stages one atomic membership delta. */
export function ClassDetailPage() {
  const { classId } = classRoute.useParams()
  const search = classRoute.useSearch()
  const navigate = classRoute.useNavigate()
  const queryClient = useQueryClient()
  const [manageOpen, setManageOpen] = useState(false)
  const [announcement, setAnnouncement] = useState('')
  const [rateOpen, setRateOpen] = useState(false)
  const [rateDate, setRateDate] = useState('')
  const [rateAmount, setRateAmount] = useState('')
  const [pendingRate, setPendingRate] = useState<{ date: string; revision: number }>()
  const retainedRateCommand = useRef<{ signature: string; key: string } | undefined>(undefined)
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
  const rateQuery = useQuery({
    queryKey: classRateKeys.detail(tutor?.tutor_id ?? '', classId),
    queryFn: ({ signal }) => readClassRates(classId, signal),
    enabled: tutor !== undefined,
    refetchInterval: (query) => {
      const pending = pendingRate
      if (pending) {
        const rate = query.state.data?.rates.find((entry) => entry.effective_from === pending.date)
        return rate && rate.rate_revision >= pending.revision ? false : 1000
      }
      const historyState = query.state.data?.history_state
      return historyState === 'syncing' || historyState === 'unavailable' ? 1000 : false
    },
  })
  const rates = rateQuery.data
  const archived = rates?.archived === true
  const historySyncing = pendingRate !== undefined || rates?.history_state === 'syncing'
  const historyUnavailable = rates?.history_state === 'unavailable'

  useEffect(() => {
    if (!search.rateDate) return
    setRateDate(search.rateDate)
    setRateOpen(true)
  }, [search.rateDate])

  useEffect(() => {
    if (!pendingRate || !rates) return
    const projected = rates.rates.find((rate) => rate.effective_from === pendingRate.date)
    if (projected?.rate_revision === pendingRate.revision) {
      setPendingRate(undefined)
      setAnnouncement('Rate history is synced with billing.')
      return
    }
    if (projected && projected.rate_revision > pendingRate.revision) {
      setPendingRate(undefined)
      setAnnouncement('A newer correction replaced this rate in billing history.')
    }
  }, [pendingRate, rates])

  const rateMutation = useMutation({
    mutationFn: async () => {
      const amount = Number(rateAmount)
      const signature = `${classId}:${rateDate}:${amount}`
      if (retainedRateCommand.current?.signature !== signature) {
        retainedRateCommand.current = { signature, key: crypto.randomUUID() }
      }
      return putClassRate(
        classId,
        rateDate,
        { rate_amount: amount },
        retainedRateCommand.current.key,
      )
    },
    onSuccess: (saved) => {
      const rateKey = classRateKeys.detail(tutor?.tutor_id ?? '', classId)
      queryClient.setQueryData<ClassRates>(rateKey, (current) =>
        current
          ? {
              ...current,
              current: saved.current,
              allowed_range: saved.allowed_range,
              history_state: 'syncing',
            }
          : current,
      )
      setPendingRate({ date: saved.effective_from, revision: saved.rate_revision })
      setAnnouncement('Rate saved. Billing history is syncing.')
      setRateOpen(false)
      retainedRateCommand.current = undefined
      void navigate({
        to: '/classes/$classId',
        params: { classId },
        search: { date: search.date, rateDate: undefined },
        replace: true,
      })
      void queryClient.invalidateQueries({
        queryKey: rateKey,
      })
      void queryClient.invalidateQueries({ queryKey: billingKeys.all })
    },
  })

  useEffect(() => {
    document.title = roster ? `${roster.class.name} roster · Vermouth` : 'Class roster · Vermouth'
  }, [roster])

  function openManage(event: ReactMouseEvent<HTMLButtonElement>) {
    manageReturnFocusRef.current = event.currentTarget
    setManageOpen(true)
  }

  const loading = tutorQuery.isPending || (rosterQuery.isPending && !archived)
  const error = tutorQuery.error ?? (archived ? null : rosterQuery.error)

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
        ) : error || (!roster && !archived) || !tutor ? (
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
              data-class-color={roster?.class.color}
              className="grid gap-5 rounded-xl border border-class-border border-s-4 border-s-class-marker bg-class-surface p-5 shadow-field sm:p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
            >
              <div className="grid gap-3">
                <Badge variant="primary" className="w-fit">
                  <ListChecks aria-hidden="true" className="size-icon-sm" />
                  {archived ? 'Archived class' : 'Dated class roster'}
                </Badge>
                <div className="grid gap-1">
                  <h1 className="text-3xl font-semibold text-balance">
                    {roster?.class.name ?? 'Archived class rates'}
                  </h1>
                  <p className="text-base leading-relaxed text-muted-foreground">
                    {archived
                      ? 'Correct rates for retained sessions. This class stays archived.'
                      : roster?.students.length === 1
                        ? '1 student covered on this date.'
                        : `${roster?.students.length ?? 0} students covered on this date.`}
                  </p>
                </div>
              </div>
              {!archived && roster ? (
                <Button size="large" className="w-full lg:w-auto" onClick={openManage}>
                  Manage roster
                </Button>
              ) : null}
            </header>

            <div
              role="status"
              aria-live="polite"
              className="text-sm font-medium text-success-foreground"
            >
              {announcement}
            </div>

            <section data-entrance-item aria-labelledby="rates-heading" className="grid gap-4">
              <div className="flex flex-col gap-4 rounded-xl border border-border bg-surface p-5 shadow-field sm:flex-row sm:items-end sm:justify-between">
                <div className="grid gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 id="rates-heading" className="text-xl font-semibold">
                      Tuition rates
                    </h2>
                    {historySyncing ? <Badge variant="warning">Syncing history</Badge> : null}
                    {historyUnavailable ? (
                      <Badge variant="warning">History unavailable</Badge>
                    ) : null}
                  </div>
                  <p className="text-sm leading-relaxed text-muted-foreground">
                    Each session uses the newest dated rate on or before its local date. Issued
                    invoices stay unchanged.
                  </p>
                  {rates ? (
                    <>
                      <p className="font-mono text-lg font-semibold">
                        {formatDong(rates.current.rate_amount)} from{' '}
                        {formatDate(rates.current.effective_from)}
                      </p>
                      {historyUnavailable ? (
                        <p role="alert" className="text-sm text-warning-foreground">
                          Billing rate history is unavailable. The confirmed teaching rate is still
                          shown.
                        </p>
                      ) : null}
                    </>
                  ) : rateQuery.isPending ? (
                    <Skeleton className="h-6 w-48" />
                  ) : (
                    <p role="alert" className="text-sm text-destructive-foreground">
                      {rateQuery.error?.message ?? 'Rate history is unavailable.'}
                    </p>
                  )}
                </div>
                <Dialog
                  open={rateOpen}
                  onOpenChange={(open) => {
                    setRateOpen(open)
                    if (open && rates) {
                      setRateDate(search.rateDate ?? rates.allowed_range.through)
                      setRateAmount(String(rates.current.rate_amount))
                    }
                    if (!open && search.rateDate) {
                      void navigate({
                        to: '/classes/$classId',
                        params: { classId },
                        search: { date: search.date, rateDate: undefined },
                        replace: true,
                      })
                    }
                  }}
                >
                  <DialogTrigger asChild>
                    <Button className="w-full sm:w-auto" disabled={!rates}>
                      Add or correct rate
                    </Button>
                  </DialogTrigger>
                  <DialogContent closeLabel="Close rate dialog">
                    <DialogHeader>
                      <DialogTitle>Set a dated tuition rate</DialogTitle>
                      <DialogDescription>
                        A same date correction creates a new audit revision. Existing invoices do
                        not change.
                      </DialogDescription>
                    </DialogHeader>
                    <form
                      className="grid gap-5"
                      onSubmit={(event) => {
                        event.preventDefault()
                        rateMutation.mutate()
                      }}
                    >
                      <FormField
                        controlId="rate-effective-date"
                        label="Effective date"
                        hint={
                          rates
                            ? `${formatDate(rates.allowed_range.from)} through ${formatDate(rates.allowed_range.through)}`
                            : undefined
                        }
                        control={(accessibility) => (
                          <Input
                            {...accessibility}
                            type="date"
                            min={rates?.allowed_range.from}
                            max={rates?.allowed_range.through}
                            value={rateDate}
                            onChange={(event) => {
                              setRateDate(event.target.value)
                              retainedRateCommand.current = undefined
                            }}
                            required
                          />
                        )}
                      />
                      <FormField
                        controlId="rate-amount"
                        label="Rate per Present session"
                        hint="Integer dong from 0 through 1,000,000,000."
                        error={rateMutation.error?.message}
                        control={(accessibility) => (
                          <Input
                            {...accessibility}
                            type="number"
                            inputMode="numeric"
                            min={0}
                            max={1_000_000_000}
                            step={1}
                            value={rateAmount}
                            onChange={(event) => {
                              setRateAmount(event.target.value)
                              retainedRateCommand.current = undefined
                            }}
                            required
                          />
                        )}
                      />
                      <DialogFooter>
                        <Button type="submit" loading={rateMutation.isPending}>
                          Save dated rate
                        </Button>
                      </DialogFooter>
                    </form>
                  </DialogContent>
                </Dialog>
              </div>

              {rates && rates.rates.length > 0 ? (
                <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  {rates.rates.map((rate) => (
                    <li key={rate.effective_from}>
                      <Card className="h-full">
                        <CardContent className="grid gap-1 p-4">
                          <p className="font-mono font-semibold">{formatDong(rate.rate_amount)}</p>
                          <p className="text-sm text-muted-foreground">
                            From {formatDate(rate.effective_from)} · revision {rate.rate_revision}
                          </p>
                        </CardContent>
                      </Card>
                    </li>
                  ))}
                </ul>
              ) : null}
            </section>

            {roster && !archived ? (
              <>
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
            ) : null}
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

function formatDong(value: number) {
  return new Intl.NumberFormat('vi-VN', {
    style: 'currency',
    currency: 'VND',
    maximumFractionDigits: 0,
  }).format(value)
}
