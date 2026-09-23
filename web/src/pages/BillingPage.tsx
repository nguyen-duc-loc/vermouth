import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  CalendarDays,
  CheckCircle2,
  FileCheck2,
  Home,
  Landmark,
  ReceiptText,
  Users,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import {
  BillingApiError,
  type BillingPreview,
  billingKeys,
  issueBillingPeriod,
  previewBillingPeriod,
  readBillingPeriod,
  readBillingPeriodDefault,
} from '../api/billing'
import { readTutor } from '../api/teaching'
import { AccountPanel } from '../components/AccountPanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FormField } from '../components/FormField'
import { PageEntrance } from '../components/PageEntrance'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Skeleton } from '../components/ui/skeleton'

const billingRoute = getRouteApi('/billing')

type IssueVariables = BillingPreview['period'] & {
  tutorId: string
  fingerprint: string
}

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
  { href: '/students', label: 'Students', icon: Users },
  { href: '/billing', label: 'Billing', icon: ReceiptText },
]

/** Reviews one complete month before money becomes immutable. */
export function BillingPage() {
  const search = billingRoute.useSearch()
  const navigate = billingRoute.useNavigate()
  const queryClient = useQueryClient()
  const [announcement, setAnnouncement] = useState('')

  useEffect(() => {
    document.title = 'Monthly billing · Vermouth'
  }, [])

  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const tutorId = tutorQuery.data?.tutor_id ?? ''
  const defaultQuery = useQuery({
    queryKey: billingKeys.defaultPeriod(tutorId),
    queryFn: ({ signal }) => readBillingPeriodDefault(signal),
    enabled: tutorId !== '',
  })

  useEffect(() => {
    const fallback = defaultQuery.data
    if (!fallback || (search.year !== undefined && search.month !== undefined)) return
    void navigate({
      to: '/billing',
      search: { year: fallback.year, month: fallback.month },
      replace: true,
    })
  }, [defaultQuery.data, navigate, search.month, search.year])

  const selected =
    search.year !== undefined && search.month !== undefined
      ? { year: search.year, month: search.month }
      : undefined
  const periodQuery = useQuery({
    queryKey: selected
      ? billingKeys.period(tutorId, selected.year, selected.month)
      : [...billingKeys.periods(tutorId), 'waiting'],
    queryFn: ({ signal }) => readBillingPeriod(selected?.year ?? 0, selected?.month ?? 0, signal),
    enabled: tutorId !== '' && selected !== undefined,
  })
  const previewQuery = useQuery({
    queryKey: selected
      ? billingKeys.preview(tutorId, selected.year, selected.month)
      : [...billingKeys.periods(tutorId), 'preview-waiting'],
    queryFn: ({ signal }) =>
      previewBillingPeriod(selected?.year ?? 0, selected?.month ?? 0, signal),
    enabled: selected !== undefined && periodQuery.data?.status === 'unissued',
    retry: (failureCount, error) =>
      error instanceof BillingApiError && error.body.error.code === 'projection_sync_pending'
        ? failureCount < 2
        : false,
  })

  const preview = previewQuery.data
  useEffect(() => {
    if (preview?.status !== 'already_issued' || !preview.run) return
    queryClient.setQueryData(
      billingKeys.period(tutorId, preview.period.year, preview.period.month),
      { status: 'already_issued', period: preview.period, run: preview.run },
    )
  }, [preview, queryClient, tutorId])

  const issueMutation = useMutation({
    mutationFn: ({ year, month, fingerprint }: IssueVariables) =>
      issueBillingPeriod(year, month, fingerprint),
    onSuccess: (run, submitted) => {
      queryClient.setQueryData(
        billingKeys.period(submitted.tutorId, run.period.year, run.period.month),
        {
          status: 'already_issued',
          period: run.period,
          run,
        },
      )
      queryClient.removeQueries({
        queryKey: billingKeys.preview(submitted.tutorId, submitted.year, submitted.month),
      })
      setAnnouncement(`Issued ${run.invoices.length} invoices for ${monthLabel(run.period)}.`)
    },
    onError: async (error, submitted) => {
      if (error instanceof BillingApiError && error.body.error.code === 'preview_stale') {
        setAnnouncement('Teaching facts changed. Review the refreshed preview before issuing.')
        await queryClient.invalidateQueries({
          queryKey: billingKeys.preview(submitted.tutorId, submitted.year, submitted.month),
        })
        return
      }
      if (error.name === 'AbortError') return

      const queryKey = billingKeys.period(submitted.tutorId, submitted.year, submitted.month)
      await queryClient.cancelQueries({ queryKey, exact: true })
      if (!queryClient.getQueryState(queryKey)) return
      setAnnouncement(`Checking whether ${monthLabel(submitted)} was issued.`)
      try {
        const recovered = await queryClient.fetchQuery({
          queryKey,
          queryFn: ({ signal }) => readBillingPeriod(submitted.year, submitted.month, signal),
          staleTime: 0,
          retry: false,
        })
        setAnnouncement(
          recovered.run
            ? `Issued ${recovered.run.invoices.length} invoices for ${monthLabel(recovered.period)}.`
            : `${monthLabel(submitted)} is unissued. Review the preview before trying again.`,
        )
      } catch {
        setAnnouncement(
          'The issue result could not be confirmed. Retry the month read before issuing.',
        )
      }
    },
  })

  const run = periodQuery.data?.run ?? (preview?.status === 'already_issued' ? preview.run : null)
  const loading =
    tutorQuery.isPending ||
    defaultQuery.isPending ||
    selected === undefined ||
    periodQuery.isPending
  const pageError = tutorQuery.error ?? defaultQuery.error ?? periodQuery.error
  const retryPage = () => {
    if (tutorQuery.error) return tutorQuery.refetch()
    if (defaultQuery.error) return defaultQuery.refetch()
    return periodQuery.refetch()
  }
  const maxMonth = defaultQuery.data
    ? monthValue(defaultQuery.data.year, defaultQuery.data.month)
    : undefined
  const selectedMonth = selected ? monthValue(selected.year, selected.month) : ''
  const grandTotal = run?.grand_total ?? preview?.grand_total ?? 0

  const contextualPanel = useMemo(
    () => (
      <div className="grid gap-5">
        <div className="grid gap-2">
          <Badge variant="primary" className="w-fit">
            Review first
          </Badge>
          <h2 className="text-lg font-semibold">A checked money boundary</h2>
          <p className="text-sm leading-relaxed text-muted-foreground">
            Preview proves the teaching projection is current. Issue recalculates the same facts
            before it freezes invoice numbers and lines.
          </p>
        </div>
        <div className="rounded-lg border border-border bg-muted p-4 text-sm leading-relaxed">
          Issued invoices stay unchanged when a rate, attendance mark, name, or bank detail changes
          later.
        </div>
      </div>
    ),
    [],
  )

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to monthly billing',
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
      <PageEntrance className="mx-auto grid w-full max-w-6xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-end"
        >
          <div className="grid gap-3">
            <Badge variant="primary" className="w-fit">
              <ReceiptText aria-hidden="true" className="size-icon-sm" />
              Month end review
            </Badge>
            <div className="grid gap-2">
              <h1 className="text-3xl font-semibold text-balance">Turn attendance into tuition.</h1>
              <p className="max-w-3xl text-base leading-relaxed text-muted-foreground">
                Review every Present session, clear anything missing, then issue one immutable
                invoice per student.
              </p>
            </div>
          </div>
          <FormField
            controlId="billing-month"
            label="Completed month"
            hint="The selected month stays in this page address."
            control={(accessibility) => (
              <Input
                {...accessibility}
                type="month"
                min="2000-01"
                max={maxMonth}
                value={selectedMonth}
                onChange={(event) => {
                  const parsed = parseMonthValue(event.target.value)
                  if (parsed) void navigate({ to: '/billing', search: parsed })
                }}
              />
            )}
          />
        </header>

        <div
          role="status"
          aria-live="polite"
          className="min-h-5 text-sm font-medium text-success-foreground"
        >
          {announcement}
        </div>

        {pageError ? (
          <ErrorState
            headingLevel="h2"
            title="This billing month could not be opened"
            description={pageError.message}
            action={<Button onClick={() => void retryPage()}>Try again</Button>}
          />
        ) : loading ? (
          <BillingLoading />
        ) : (
          <>
            <section
              data-entrance-item
              aria-labelledby="month-summary"
              className="grid gap-4 sm:grid-cols-3"
            >
              <h2 id="month-summary" className="sr-only">
                Month summary
              </h2>
              <SummaryCard label="Month" value={selected ? monthLabel(selected) : 'Not selected'} />
              <SummaryCard
                label={run ? 'Issued invoices' : 'Prospective invoices'}
                value={String(run?.invoices.length ?? preview?.students.length ?? 0)}
              />
              <SummaryCard label="Reviewed total" value={formatDong(grandTotal)} mono />
            </section>

            {run ? (
              <IssuedRun run={run} />
            ) : previewQuery.isPending ? (
              <BillingLoading compact />
            ) : previewQuery.error ? (
              <ErrorState
                headingLevel="h2"
                title="The preview is not ready"
                description={previewQuery.error.message}
                action={<Button onClick={() => void previewQuery.refetch()}>Retry preview</Button>}
              />
            ) : preview ? (
              <PreviewSurface
                preview={preview}
                issuing={issueMutation.isPending || periodQuery.isFetching}
                issueError={issueMutation.error}
                onIssue={() => {
                  if (!selected || !preview.preview_fingerprint) return
                  issueMutation.mutate({
                    tutorId,
                    ...selected,
                    fingerprint: preview.preview_fingerprint,
                  })
                }}
              />
            ) : null}
          </>
        )}
      </PageEntrance>
    </AppShell>
  )
}

function PreviewSurface({
  preview,
  issuing,
  issueError,
  onIssue,
}: {
  preview: Awaited<ReturnType<typeof previewBillingPeriod>>
  issuing: boolean
  issueError: Error | null
  onIssue: () => void
}) {
  if (preview.status === 'empty') {
    return (
      <EmptyState
        icon={<CalendarDays aria-hidden="true" className="size-icon-lg" />}
        title="No invoices for this month"
        description="Every attendance obligation is complete, but no student has a Present session to bill."
      />
    )
  }

  return (
    <div className="grid gap-6">
      {preview.blockers.length > 0 ? (
        <section aria-labelledby="blockers-heading" className="grid gap-4">
          <Card className="border-warning bg-warning-surface">
            <CardHeader>
              <div className="flex items-start gap-3">
                <AlertTriangle
                  aria-hidden="true"
                  className="mt-1 size-icon-md shrink-0 text-warning"
                />
                <div className="grid gap-1">
                  <CardTitle id="blockers-heading">Clear these items before issue</CardTitle>
                  <CardDescription className="text-warning-foreground">
                    The preview stays available for review, but no money record has been written.
                  </CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <ul className="grid gap-3">
                {preview.blockers.map((blocker, index) => (
                  <li key={`${blocker.code}-${blocker.session_id ?? blocker.field ?? index}`}>
                    <RecoveryLink blocker={blocker} />
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
        </section>
      ) : null}

      <section aria-labelledby="invoice-review-heading" className="grid gap-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="grid gap-1">
            <h2 id="invoice-review-heading" className="text-xl font-semibold">
              Student invoice review
            </h2>
            <p className="text-sm text-muted-foreground">
              Present sessions only. Absent and cancelled sessions are excluded.
            </p>
          </div>
          <Button
            size="large"
            loading={issuing}
            disabled={preview.status !== 'ready' || preview.preview_fingerprint === null}
            onClick={onIssue}
          >
            Issue {preview.students.length} invoices
          </Button>
        </div>
        {issueError ? (
          <p
            role="alert"
            className="rounded-lg border border-destructive bg-destructive-surface p-4 text-sm text-destructive-foreground"
          >
            {issueError.message}
          </p>
        ) : null}
        <div className="grid gap-4">
          {preview.students.map((student) => (
            <StudentInvoiceCard key={student.student_id} student={student} />
          ))}
        </div>
      </section>
    </div>
  )
}

function IssuedRun({
  run,
}: {
  run: NonNullable<Awaited<ReturnType<typeof readBillingPeriod>>['run']>
}) {
  return (
    <section aria-labelledby="issued-heading" className="grid gap-4">
      <Card className="border-success bg-success-surface">
        <CardHeader>
          <div className="flex items-start gap-3">
            <CheckCircle2 aria-hidden="true" className="mt-1 size-icon-md shrink-0 text-success" />
            <div className="grid gap-1">
              <CardTitle id="issued-heading">This month is issued</CardTitle>
              <CardDescription className="text-success-foreground">
                These numbers, names, lines, and totals are frozen. PDF files arrive in the next
                billing feature.
              </CardDescription>
            </div>
          </div>
        </CardHeader>
      </Card>
      {run.invoices.map((invoice) => (
        <Card key={invoice.invoice_id}>
          <CardHeader className="sm:flex-row sm:items-start sm:justify-between">
            <div>
              <CardTitle>{invoice.student_name}</CardTitle>
              <CardDescription>{invoice.invoice_number}</CardDescription>
            </div>
            <p className="font-mono text-lg font-semibold">{formatDong(invoice.total_amount)}</p>
          </CardHeader>
          <CardContent>
            <LineList lines={invoice.lines} />
          </CardContent>
        </Card>
      ))}
    </section>
  )
}

function StudentInvoiceCard({
  student,
}: {
  student: Awaited<ReturnType<typeof previewBillingPeriod>>['students'][number]
}) {
  return (
    <Card>
      <CardHeader className="sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>{student.student_name}</CardTitle>
          <CardDescription>
            {student.lines.length} Present {student.lines.length === 1 ? 'session' : 'sessions'}
          </CardDescription>
        </div>
        <p className="font-mono text-lg font-semibold">{formatDong(student.total_amount)}</p>
      </CardHeader>
      <CardContent>
        <LineList lines={student.lines} />
      </CardContent>
    </Card>
  )
}

function LineList({
  lines,
}: {
  lines: readonly Awaited<
    ReturnType<typeof previewBillingPeriod>
  >['students'][number]['lines'][number][]
}) {
  return (
    <ul className="divide-y divide-border rounded-lg border border-border">
      {lines.map((line) => (
        <li
          key={line.session_id}
          className="grid gap-1 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
        >
          <div>
            <p className="font-medium">{line.class_name}</p>
            <p className="text-sm text-muted-foreground">{formatDate(line.local_date)}</p>
          </div>
          <p className="font-mono text-sm font-semibold">{formatDong(line.amount)}</p>
        </li>
      ))}
    </ul>
  )
}

function RecoveryLink({
  blocker,
}: {
  blocker: Awaited<ReturnType<typeof previewBillingPeriod>>['blockers'][number]
}) {
  const content = blockerText(blocker)
  const className =
    'flex min-h-11 items-center justify-between gap-3 rounded-lg border border-warning bg-surface px-3 py-2 text-sm font-medium outline-none transition-colors duration-base hover:bg-muted focus-visible:ring-2 focus-visible:ring-focus motion-reduce:transition-none'
  if (blocker.code === 'profile_incomplete') {
    return (
      <Link to="/profile" className={className}>
        <span>{content}</span>
        <Landmark aria-hidden="true" className="size-icon-sm shrink-0" />
      </Link>
    )
  }
  if (blocker.code === 'rate_missing' && blocker.class_id && blocker.local_date) {
    return (
      <Link
        to="/classes/$classId"
        params={{ classId: blocker.class_id }}
        search={{ date: undefined, rateDate: blocker.local_date }}
        className={className}
      >
        <span>{content}</span>
        <ReceiptText aria-hidden="true" className="size-icon-sm shrink-0" />
      </Link>
    )
  }
  return (
    <Link
      to="/"
      search={{ date: blocker.local_date ?? undefined, session: blocker.session_id ?? undefined }}
      className={className}
    >
      <span>{content}</span>
      <FileCheck2 aria-hidden="true" className="size-icon-sm shrink-0" />
    </Link>
  )
}

function blockerText(
  blocker: Awaited<ReturnType<typeof previewBillingPeriod>>['blockers'][number],
) {
  if (blocker.code === 'profile_incomplete')
    return `Complete profile field: ${blocker.field ?? 'invoice details'}`
  if (blocker.code === 'rate_missing')
    return `Add a rate for ${blocker.local_date ?? 'this session'}`
  return `Mark attendance for ${blocker.local_date ?? 'this session'}`
}

function SummaryCard({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <Card>
      <CardContent className="grid gap-1 p-5">
        <p className="text-sm text-muted-foreground">{label}</p>
        <p className={mono ? 'font-mono text-xl font-semibold' : 'text-xl font-semibold'}>
          {value}
        </p>
      </CardContent>
    </Card>
  )
}

function BillingLoading({ compact = false }: { compact?: boolean }) {
  return (
    <Card role="status" aria-label="Loading billing month" aria-busy="true">
      <CardHeader className="gap-3">
        <Skeleton className="h-7 w-2/5" />
        <Skeleton className="h-4 w-3/5" />
      </CardHeader>
      <CardContent className="grid gap-3">
        <Skeleton className={compact ? 'h-24 w-full' : 'h-32 w-full'} />
        <Skeleton className="h-24 w-full" />
      </CardContent>
    </Card>
  )
}

function parseMonthValue(value: string) {
  const match = /^(\d{4})-(\d{2})$/.exec(value)
  if (!match) return undefined
  const year = Number(match[1])
  const month = Number(match[2])
  if (!Number.isInteger(year) || year < 2000 || month < 1 || month > 12) return undefined
  return { year, month }
}

function monthValue(year: number, month: number) {
  return `${year}-${String(month).padStart(2, '0')}`
}

function monthLabel(period: { year: number; month: number }) {
  return new Intl.DateTimeFormat('en', { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(
    new Date(Date.UTC(period.year, period.month - 1, 1)),
  )
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
