import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { Archive, CalendarDays, Home, Pencil, Users } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import {
  archiveStudent,
  type ClassReference,
  readStudent,
  readTutor,
  type Student,
  type StudentDetail,
  type StudentMembership,
  studentKeys,
  TeachingApiError,
  updateStudent,
} from '../api/teaching'
import { AccountPanel } from '../components/AccountPanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { ErrorState } from '../components/ErrorState'
import { PageEntrance } from '../components/PageEntrance'
import { StudentEditSheet } from '../components/StudentEditSheet'
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
} from '../components/ui/dialog'
import { Skeleton } from '../components/ui/skeleton'

const studentRoute = getRouteApi('/students/$studentId')

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
  { href: '/students', label: 'Students', icon: Users },
]

/** Shows one private student record above its retained class history. */
export function StudentDetailPage() {
  const { studentId } = studentRoute.useParams()
  const navigate = studentRoute.useNavigate()
  const queryClient = useQueryClient()
  const [editOpen, setEditOpen] = useState(false)
  const [archiveOpen, setArchiveOpen] = useState(false)
  const [announcement, setAnnouncement] = useState('')
  const [blockedClasses, setBlockedClasses] = useState<ClassReference[]>([])
  const editReturnFocusRef = useRef<HTMLButtonElement>(null)
  const archiveReturnFocusRef = useRef<HTMLButtonElement>(null)
  const archiveKey = useRef<string | undefined>(undefined)

  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const detailQuery = useQuery({
    queryKey: studentKeys.detail(tutorQuery.data?.tutor_id ?? '', studentId),
    queryFn: ({ signal }) => readStudent(studentId, signal),
    enabled: tutorQuery.data !== undefined,
  })
  const detail = detailQuery.data

  useEffect(() => {
    document.title = detail ? `${detail.student.name} · Vermouth` : 'Student · Vermouth'
  }, [detail])

  const updateMutation = useMutation({
    mutationFn: ({ input, key }: { input: Parameters<typeof updateStudent>[1]; key: string }) =>
      updateStudent(studentId, input, key),
    onSuccess: async (student) => {
      queryClient.setQueryData<StudentDetail>(
        studentKeys.detail(tutorQuery.data?.tutor_id ?? '', studentId),
        (current) => (current ? { ...current, student } : current),
      )
      await queryClient.invalidateQueries({ queryKey: studentKeys.lists() })
    },
  })

  const archiveMutation = useMutation({
    mutationFn: async () => {
      archiveKey.current ??= crypto.randomUUID()
      await archiveStudent(studentId, archiveKey.current)
    },
    onSuccess: async () => {
      archiveKey.current = undefined
      await queryClient.invalidateQueries({ queryKey: studentKeys.all })
      await navigate({ to: '/students', search: {} })
    },
    onError: (error) => {
      if (error instanceof TeachingApiError && error.body.error.code === 'active_memberships') {
        setBlockedClasses(readBlockedClasses(error.body.error.details))
      }
    },
  })

  const activeMemberships = detail?.memberships.filter((membership) => membership.active) ?? []
  const pastMemberships = detail?.memberships.filter((membership) => !membership.active) ?? []
  const loading = tutorQuery.isPending || detailQuery.isPending
  const error = tutorQuery.error ?? detailQuery.error

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to student record',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Change this device appearance.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={<AccountPanel />}
    >
      <PageEntrance className="mx-auto grid w-full max-w-5xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        {loading ? (
          <Card role="status" aria-label="Loading student record" aria-busy="true">
            <CardHeader className="gap-3">
              <Skeleton className="h-7 w-2/5" />
              <Skeleton className="h-4 w-3/5" />
            </CardHeader>
            <CardContent className="grid gap-3">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
            </CardContent>
          </Card>
        ) : error || !detail ? (
          <ErrorState
            headingLevel="h2"
            title="This student could not be read"
            description={error?.message ?? 'No active student matched this address.'}
            action={
              <Button asChild variant="secondary">
                <Link to="/students" search={{}}>
                  Return to students
                </Link>
              </Button>
            }
          />
        ) : (
          <>
            <header
              data-entrance-item
              className="grid gap-5 rounded-xl border border-border bg-surface p-5 shadow-field sm:p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
            >
              <div className="grid gap-3">
                <Badge variant="primary" className="w-fit">
                  <Users aria-hidden="true" className="size-icon-sm" />
                  Active student
                </Badge>
                <div className="grid gap-1">
                  <h1 className="text-3xl font-semibold text-balance">{detail.student.name}</h1>
                  <p className="text-base text-muted-foreground">
                    {detail.student.phone ?? 'No phone saved'}
                  </p>
                </div>
                <p className="text-sm text-muted-foreground">
                  Updated {formatInstant(detail.student.updated_at)}
                </p>
              </div>
              <div className="flex flex-col gap-3 sm:flex-row">
                <Button
                  variant="secondary"
                  onClick={(event) => {
                    editReturnFocusRef.current = event.currentTarget
                    setEditOpen(true)
                  }}
                >
                  <Pencil aria-hidden="true" className="size-icon-sm" />
                  Edit student
                </Button>
                <Button
                  variant="destructive"
                  onClick={(event) => {
                    archiveReturnFocusRef.current = event.currentTarget
                    setBlockedClasses([])
                    setArchiveOpen(true)
                  }}
                >
                  <Archive aria-hidden="true" className="size-icon-sm" />
                  Archive student
                </Button>
              </div>
            </header>

            <div
              role="status"
              aria-live="polite"
              className="text-sm font-medium text-success-foreground"
            >
              {announcement}
            </div>

            <MembershipSection
              title="Active classes"
              memberships={activeMemberships}
              empty="This student is not in an active class today."
            />
            <MembershipSection
              title="Past memberships"
              memberships={pastMemberships}
              empty="No past class membership is retained yet."
            />

            <StudentEditSheet
              open={editOpen}
              student={detail.student}
              onOpenChange={setEditOpen}
              returnFocusRef={editReturnFocusRef}
              onUpdate={(input, key) => updateMutation.mutateAsync({ input, key })}
              onReadCurrent={async () => (await readStudent(studentId)).student}
              onUpdated={(student: Student) => setAnnouncement(`${student.name} was updated.`)}
            />

            <Dialog open={archiveOpen} onOpenChange={setArchiveOpen}>
              <DialogContent
                closeLabel="Close archive confirmation"
                onCloseAutoFocus={(event) => {
                  event.preventDefault()
                  archiveReturnFocusRef.current?.focus()
                }}
              >
                <DialogHeader>
                  <DialogTitle>Archive {detail.student.name}?</DialogTitle>
                  <DialogDescription>
                    The record will leave active student lists. Teaching history keeps the name, but
                    there is no restore action in this feature.
                  </DialogDescription>
                </DialogHeader>

                {archiveMutation.error && (
                  <div
                    role="alert"
                    className="grid gap-3 rounded-lg border border-destructive/35 bg-destructive-surface p-4 text-sm text-destructive-foreground"
                  >
                    <p className="font-medium">{archiveMutation.error.message}</p>
                    {blockedClasses.length > 0 && (
                      <ul className="grid gap-2">
                        {blockedClasses.map((classItem) => (
                          <li key={classItem.class_id}>
                            <Link
                              to="/classes/$classId"
                              params={{ classId: classItem.class_id }}
                              className="inline-flex min-h-11 items-center rounded-md underline underline-offset-4 outline-none focus-visible:ring-2 focus-visible:ring-focus"
                            >
                              Open {classItem.name}
                            </Link>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}

                <DialogFooter>
                  <Button variant="secondary" onClick={() => setArchiveOpen(false)}>
                    Keep student
                  </Button>
                  <Button
                    variant="destructive"
                    loading={archiveMutation.isPending}
                    onClick={() => archiveMutation.mutate()}
                  >
                    {archiveMutation.isPending ? 'Archiving student…' : 'Archive student'}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </>
        )}
      </PageEntrance>
    </AppShell>
  )
}

function MembershipSection({
  title,
  memberships,
  empty,
}: {
  title: string
  memberships: readonly StudentMembership[]
  empty: string
}) {
  return (
    <section
      data-entrance-item
      className="grid gap-3"
      aria-labelledby={`${title.replaceAll(' ', '-').toLowerCase()}-heading`}
    >
      <h2
        id={`${title.replaceAll(' ', '-').toLowerCase()}-heading`}
        className="text-xl font-semibold"
      >
        {title}
      </h2>
      {memberships.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border-strong bg-muted/55 p-5 text-sm text-muted-foreground">
          {empty}
        </p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {memberships.map((membership) => (
            <li key={`${membership.class_id}-${membership.effective_from}`}>
              <Card
                data-class-color={membership.class_color}
                className="h-full border-s-4 border-s-class-marker bg-class-surface"
              >
                <CardHeader>
                  <CardTitle>
                    <Link
                      to="/classes/$classId"
                      params={{ classId: membership.class_id }}
                      className="rounded-sm outline-none hover:underline focus-visible:ring-2 focus-visible:ring-focus"
                    >
                      {membership.class_name}
                    </Link>
                  </CardTitle>
                </CardHeader>
                <CardContent className="text-sm text-muted-foreground">
                  {formatDate(membership.effective_from)} to{' '}
                  {membership.effective_to ? formatDate(membership.effective_to) : 'today'}
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function readBlockedClasses(details: Record<string, unknown> | undefined): ClassReference[] {
  if (!details || !Array.isArray(details.classes)) return []
  return details.classes.flatMap((value) => {
    if (!value || typeof value !== 'object') return []
    const classId = 'class_id' in value ? value.class_id : undefined
    const name = 'name' in value ? value.name : undefined
    return typeof classId === 'string' && typeof name === 'string'
      ? [{ class_id: classId, name }]
      : []
  })
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeZone: 'UTC' }).format(
    new Date(`${value}T00:00:00Z`),
  )
}

function formatInstant(value: string) {
  return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}
