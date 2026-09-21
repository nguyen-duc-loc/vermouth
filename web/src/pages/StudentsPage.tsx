import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { CalendarDays, Home, Plus, Search, UserRoundPlus, Users } from 'lucide-react'
import {
  type FormEvent,
  type MouseEvent as ReactMouseEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'

import {
  createStudent,
  readStudents,
  readTutor,
  type Student,
  type StudentSummary,
  studentKeys,
} from '../api/teaching'
import { AppearancePanel, type AppearancePanelText } from '../components/AppearancePanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PageEntrance } from '../components/PageEntrance'
import { ResponsiveTable } from '../components/ResponsiveTable'
import { StudentFormSheet } from '../components/StudentFormSheet'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '../components/ui/card'
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

const studentsRoute = getRouteApi('/students')

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
  { href: '/students', label: 'Students', icon: Users },
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

/** Gives the tutor a searchable, durable student record workspace. */
export function StudentsPage() {
  const search = studentsRoute.useSearch()
  const navigate = studentsRoute.useNavigate()
  const queryClient = useQueryClient()
  const [queryDraft, setQueryDraft] = useState(search.q ?? '')
  const [createOpen, setCreateOpen] = useState(false)
  const [announcement, setAnnouncement] = useState('')
  const createReturnFocusRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    document.title = 'Students · Vermouth'
  }, [])

  useEffect(() => {
    setQueryDraft(search.q ?? '')
  }, [search.q])

  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const query = search.q ?? ''
  const studentsQuery = useInfiniteQuery({
    queryKey: studentKeys.list(tutorQuery.data?.tutor_id ?? '', query, search.cursor),
    queryFn: ({ pageParam, signal }) => readStudents(query, pageParam, signal),
    initialPageParam: search.cursor,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: tutorQuery.data !== undefined,
  })
  const students = useMemo(
    () => studentsQuery.data?.pages.flatMap((page) => page.students) ?? [],
    [studentsQuery.data],
  )

  const createMutation = useMutation({
    mutationFn: ({ input, key }: { input: Parameters<typeof createStudent>[0]; key: string }) =>
      createStudent(input, key),
    onSuccess: async () => {
      await navigate({ to: '/students', search: { q: search.q } })
      await queryClient.invalidateQueries({ queryKey: studentKeys.lists() })
    },
  })

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextQuery = queryDraft.trim()
    void navigate({
      to: '/students',
      search: nextQuery ? { q: nextQuery } : {},
    })
  }

  function openCreate(event: ReactMouseEvent<HTMLButtonElement>) {
    createReturnFocusRef.current = event.currentTarget
    setCreateOpen(true)
  }

  const loading = tutorQuery.isPending || studentsQuery.isPending
  const error = tutorQuery.error ?? studentsQuery.error

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to students',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Change this device appearance.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={<AppearancePanel text={appearanceText} />}
    >
      <PageEntrance className="mx-auto grid w-full max-w-7xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
        >
          <div className="grid max-w-3xl gap-3">
            <Badge variant="primary" className="w-fit">
              <Users aria-hidden="true" className="size-icon-sm" />
              Student records
            </Badge>
            <h1 className="text-3xl font-semibold text-balance">Know every learner by name.</h1>
            <p className="text-base leading-relaxed text-muted-foreground">
              Find contact details quickly, then open one record to see the classes that shape their
              teaching history.
            </p>
          </div>
          <Button size="large" className="w-full lg:w-auto" onClick={openCreate}>
            <Plus aria-hidden="true" className="size-icon-md" />
            Create student
          </Button>
        </header>

        <div
          role="status"
          aria-live="polite"
          className="text-sm font-medium text-success-foreground"
        >
          {announcement}
        </div>

        <section data-entrance-item aria-labelledby="student-list-heading" className="grid gap-4">
          <div className="grid gap-4 rounded-xl border border-border bg-surface p-4 shadow-field sm:p-5">
            <div className="grid gap-1">
              <h2 id="student-list-heading" className="text-xl font-semibold">
                Active students
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                Search by name or a literal phone fragment. Archived records stay only in teaching
                history.
              </p>
            </div>
            <form className="flex flex-col gap-3 sm:flex-row" onSubmit={submitSearch}>
              <label htmlFor="student-search" className="sr-only">
                Search students
              </label>
              <div className="relative min-w-0 flex-1">
                <Search
                  aria-hidden="true"
                  className="pointer-events-none absolute start-3 top-1/2 size-icon-sm -translate-y-1/2 text-muted-foreground"
                />
                <Input
                  id="student-search"
                  type="search"
                  value={queryDraft}
                  maxLength={160}
                  onChange={(event) => setQueryDraft(event.target.value)}
                  className="ps-10"
                  placeholder="Name or phone"
                />
              </div>
              <Button type="submit" variant="secondary">
                Search students
              </Button>
            </form>
          </div>

          {loading ? (
            <Card role="status" aria-label="Loading students" aria-busy="true">
              <CardHeader className="gap-3">
                <Skeleton className="h-6 w-2/5" />
                <Skeleton className="h-4 w-3/4" />
              </CardHeader>
              <CardContent className="grid gap-3">
                <Skeleton className="h-24 w-full" />
                <Skeleton className="h-24 w-full" />
              </CardContent>
            </Card>
          ) : error ? (
            <ErrorState
              headingLevel="h2"
              title="Students could not be read"
              description={error.message}
              action={
                <Button variant="secondary" onClick={() => void studentsQuery.refetch()}>
                  Try the student list again
                </Button>
              }
            />
          ) : (
            <ResponsiveTable
              rows={students}
              getRowKey={(student) => student.student_id}
              emptyState={
                <EmptyState
                  icon={<UserRoundPlus aria-hidden="true" className="size-icon-lg" />}
                  title={
                    query ? 'No active student matches this search' : 'Create the first student'
                  }
                  description={
                    query
                      ? 'Try another name or phone fragment.'
                      : 'A student record can belong to more than one class while keeping one contact detail.'
                  }
                  action={
                    query ? (
                      <Button
                        variant="secondary"
                        onClick={() => {
                          setQueryDraft('')
                          void navigate({ to: '/students', search: {} })
                        }}
                      >
                        Clear search
                      </Button>
                    ) : (
                      <Button onClick={openCreate}>Create student</Button>
                    )
                  }
                />
              }
              renderCard={(student) => <StudentCard student={student} />}
              renderTable={(rows) => <StudentTable students={rows} />}
            />
          )}

          {studentsQuery.hasNextPage && (
            <Button
              variant="secondary"
              loading={studentsQuery.isFetchingNextPage}
              onClick={() => void studentsQuery.fetchNextPage()}
              className="w-full sm:w-fit"
            >
              Load more students
            </Button>
          )}
        </section>
      </PageEntrance>

      <StudentFormSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        returnFocusRef={createReturnFocusRef}
        onCreate={(input, key) => createMutation.mutateAsync({ input, key })}
        onCreated={(student: Student) => {
          setAnnouncement(`${student.name} was created.`)
        }}
      />
    </AppShell>
  )
}

function StudentCard({ student }: { student: StudentSummary }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{student.name}</CardTitle>
        <p className="text-sm text-muted-foreground">{student.phone ?? 'No phone saved'}</p>
      </CardHeader>
      <CardContent className="text-sm text-muted-foreground">
        {classCountLabel(student.active_class_count)}
      </CardContent>
      <CardFooter>
        <Button asChild variant="secondary">
          <Link to="/students/$studentId" params={{ studentId: student.student_id }}>
            Open student
          </Link>
        </Button>
      </CardFooter>
    </Card>
  )
}

function StudentTable({ students }: { students: readonly StudentSummary[] }) {
  return (
    <Card className="overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead scope="col">Student</TableHead>
            <TableHead scope="col">Phone</TableHead>
            <TableHead scope="col">Active classes</TableHead>
            <TableHead scope="col">Updated</TableHead>
            <TableHead scope="col">
              <span className="sr-only">Open record</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {students.map((student) => (
            <TableRow key={student.student_id}>
              <TableCell className="font-medium">{student.name}</TableCell>
              <TableCell>{student.phone ?? 'Not saved'}</TableCell>
              <TableCell>{student.active_class_count}</TableCell>
              <TableCell>{formatUpdatedAt(student.updated_at)}</TableCell>
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

function classCountLabel(count: number) {
  return count === 1 ? '1 active class' : `${count} active classes`
}

function formatUpdatedAt(value: string) {
  return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}
