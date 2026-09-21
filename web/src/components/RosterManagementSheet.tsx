import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Search } from 'lucide-react'
import { type RefObject, useMemo, useRef, useState } from 'react'

import {
  type ClassRoster,
  changeClassRoster,
  createStudent,
  readStudents,
  type Student,
  type StudentSummary,
  studentKeys,
  TeachingApiError,
} from '../api/teaching'
import { FormField } from './FormField'
import { StudentFormSheet } from './StudentFormSheet'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Checkbox } from './ui/checkbox'
import { Input } from './ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from './ui/sheet'

export type RosterManagementSheetProps = {
  open: boolean
  tutorId: string
  roster: ClassRoster
  onOpenChange: (open: boolean) => void
  onSaved: (roster: ClassRoster) => void
  onConflict: () => Promise<void>
  returnFocusRef: RefObject<HTMLButtonElement | null>
}

type Candidate = Pick<StudentSummary, 'student_id' | 'name' | 'phone'> & { archived?: boolean }

/** Stages one dated roster delta while nested student creation preserves the draft. */
export function RosterManagementSheet({
  open,
  tutorId,
  roster,
  onOpenChange,
  onSaved,
  onConflict,
  returnFocusRef,
}: RosterManagementSheetProps) {
  const queryClient = useQueryClient()
  const [query, setQuery] = useState('')
  const [changeDate, setChangeDate] = useState(roster.resolved_date)
  const [additions, setAdditions] = useState<Set<string>>(() => new Set())
  const [removals, setRemovals] = useState<Set<string>>(() => new Set())
  const [createOpen, setCreateOpen] = useState(false)
  const [boundaryError, setBoundaryError] = useState<string>()
  const retainedCommand = useRef<{ signature: string; key: string } | undefined>(undefined)
  const createReturnFocusRef = useRef<HTMLButtonElement>(null)

  const studentsQuery = useInfiniteQuery({
    queryKey: studentKeys.list(tutorId, query),
    queryFn: ({ pageParam, signal }) => readStudents(query, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: open,
  })
  const searchedStudents = useMemo(
    () => studentsQuery.data?.pages.flatMap((page) => page.students) ?? [],
    [studentsQuery.data],
  )
  const candidates = useMemo(() => {
    const byID = new Map<string, Candidate>()
    for (const student of roster.students) byID.set(student.student_id, student)
    for (const student of searchedStudents) byID.set(student.student_id, student)
    return [...byID.values()].sort((left, right) =>
      left.name.localeCompare(right.name, undefined, { sensitivity: 'base' }),
    )
  }, [roster.students, searchedStudents])
  const currentIDs = useMemo(
    () => new Set(roster.students.map((student) => student.student_id)),
    [roster.students],
  )

  const saveMutation = useMutation({
    mutationFn: async () => {
      const effectiveAdditions = [...additions]
        .filter((studentId) => !currentIDs.has(studentId))
        .sort()
      const effectiveRemovals = [...removals]
        .filter((studentId) => currentIDs.has(studentId))
        .sort()
      if (effectiveAdditions.length + effectiveRemovals.length === 0) {
        throw new Error('Every staged change is already satisfied by the current roster.')
      }
      const input = {
        change_date: changeDate,
        additions: effectiveAdditions,
        removals: effectiveRemovals,
      }
      const signature = JSON.stringify(input)
      if (retainedCommand.current?.signature !== signature) {
        retainedCommand.current = { signature, key: crypto.randomUUID() }
      }
      return changeClassRoster(roster.class.class_id, input, retainedCommand.current.key)
    },
    onSuccess: async (saved) => {
      retainedCommand.current = undefined
      setAdditions(new Set())
      setRemovals(new Set())
      setBoundaryError(undefined)
      onSaved(saved)
      onOpenChange(false)
    },
    onError: async (error) => {
      if (error instanceof TeachingApiError && error.body.error.code === 'roster_conflict') {
        retainedCommand.current = undefined
        await onConflict()
        setBoundaryError(
          'The roster changed elsewhere. Satisfied choices are marked below. Review the remaining delta before saving again.',
        )
        return
      }
      setBoundaryError(error instanceof Error ? error.message : 'The roster could not be changed.')
    },
  })

  function checked(studentId: string) {
    if (additions.has(studentId)) return true
    if (removals.has(studentId)) return false
    return currentIDs.has(studentId)
  }

  function toggle(studentId: string, nextChecked: boolean) {
    setBoundaryError(undefined)
    retainedCommand.current = undefined
    if (currentIDs.has(studentId)) {
      setRemovals((current) => changedSet(current, studentId, !nextChecked))
      setAdditions((current) => changedSet(current, studentId, false))
      return
    }
    setAdditions((current) => changedSet(current, studentId, nextChecked))
    setRemovals((current) => changedSet(current, studentId, false))
  }

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent
          side="end"
          closeLabel="Close roster management"
          className="w-full sm:max-w-xl"
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            returnFocusRef.current?.focus()
          }}
        >
          <SheetHeader>
            <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
              {roster.class.name}
            </p>
            <SheetTitle>Manage roster</SheetTitle>
            <SheetDescription>
              One save applies every checked addition and removal on the same local date. A student
              removed on that date leaves on the previous date.
            </SheetDescription>
          </SheetHeader>

          <FormField
            controlId="roster-change-date"
            label="First date with the new roster"
            hint="A same day addition cannot be erased. The earliest removal is the following day."
            control={(accessibility) => (
              <Input
                {...accessibility}
                type="date"
                value={changeDate}
                onChange={(event) => {
                  setChangeDate(event.target.value)
                  retainedCommand.current = undefined
                }}
              />
            )}
          />

          <div className="grid gap-3">
            <div className="flex gap-2">
              <label htmlFor="roster-student-search" className="sr-only">
                Search active students
              </label>
              <div className="relative min-w-0 flex-1">
                <Search
                  aria-hidden="true"
                  className="pointer-events-none absolute start-3 top-1/2 size-icon-sm -translate-y-1/2 text-muted-foreground"
                />
                <Input
                  id="roster-student-search"
                  type="search"
                  value={query}
                  maxLength={160}
                  onChange={(event) => setQuery(event.target.value.trimStart())}
                  className="ps-10"
                  placeholder="Find an active student"
                />
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={(event) => {
                  createReturnFocusRef.current = event.currentTarget
                  setCreateOpen(true)
                }}
              >
                <Plus aria-hidden="true" className="size-icon-sm" />
                Create
              </Button>
            </div>

            {studentsQuery.error && (
              <p role="alert" className="text-sm font-medium text-destructive">
                {studentsQuery.error.message}
              </p>
            )}

            <fieldset className="grid gap-2">
              <legend className="mb-1 text-sm font-medium">Students in this roster</legend>
              {candidates.map((student) => {
                const isChecked = checked(student.student_id)
                const satisfiedAddition =
                  additions.has(student.student_id) && currentIDs.has(student.student_id)
                const satisfiedRemoval =
                  removals.has(student.student_id) && !currentIDs.has(student.student_id)
                return (
                  <label
                    key={student.student_id}
                    htmlFor={`roster-student-${student.student_id}`}
                    className="flex min-h-14 cursor-pointer items-center gap-3 rounded-lg border border-border bg-surface p-3 outline-none focus-within:ring-2 focus-within:ring-focus"
                  >
                    <Checkbox
                      id={`roster-student-${student.student_id}`}
                      checked={isChecked}
                      disabled={student.archived}
                      onCheckedChange={(value) => toggle(student.student_id, value === true)}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block font-medium wrap-anywhere">{student.name}</span>
                      <span className="block text-sm text-muted-foreground">
                        {student.phone ?? (student.archived ? 'Archived' : 'No phone saved')}
                      </span>
                    </span>
                    {(satisfiedAddition || satisfiedRemoval) && <Badge>Already satisfied</Badge>}
                  </label>
                )
              })}
            </fieldset>

            {studentsQuery.hasNextPage && (
              <Button
                type="button"
                variant="secondary"
                loading={studentsQuery.isFetchingNextPage}
                onClick={() => void studentsQuery.fetchNextPage()}
              >
                Load more students
              </Button>
            )}
          </div>

          {boundaryError && (
            <p
              role="alert"
              className="rounded-lg border border-destructive/35 bg-destructive-surface p-4 text-sm font-medium text-destructive-foreground"
            >
              {boundaryError}
            </p>
          )}

          <SheetFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Keep current roster
            </Button>
            <Button
              type="button"
              loading={saveMutation.isPending}
              disabled={additions.size + removals.size === 0}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? 'Saving roster…' : 'Save roster'}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <StudentFormSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        returnFocusRef={createReturnFocusRef}
        onCreate={(input, key) => createStudent(input, key)}
        onCreated={(student: Student) => {
          setAdditions((current) => changedSet(current, student.student_id, true))
          void queryClient.invalidateQueries({ queryKey: studentKeys.lists() })
        }}
      />
    </>
  )
}

function changedSet(current: Set<string>, value: string, present: boolean) {
  const next = new Set(current)
  if (present) next.add(value)
  else next.delete(value)
  return next
}
