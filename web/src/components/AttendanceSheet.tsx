import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CheckCheck } from 'lucide-react'
import { type RefObject, useEffect, useRef, useState } from 'react'

import {
  type AttendanceState,
  attendanceKeys,
  readAttendance,
  saveAttendance,
  TeachingApiError,
} from '../api/teaching'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { RadioGroup, RadioGroupItem } from './ui/radio-group'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from './ui/sheet'
import { Skeleton } from './ui/skeleton'

export type AttendanceSheetProps = {
  open: boolean
  tutorId: string
  sessionId?: string
  onOpenChange: (open: boolean) => void
  onSaved: () => void
  returnFocusRef: RefObject<HTMLButtonElement | null>
}

/** Reads and saves one whole roster attendance pass with stale review recovery. */
export function AttendanceSheet({
  open,
  tutorId,
  sessionId,
  onOpenChange,
  onSaved,
  returnFocusRef,
}: AttendanceSheetProps) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Record<string, AttendanceState | undefined>>({})
  const [baselineRevision, setBaselineRevision] = useState<string>()
  const [requiresReview, setRequiresReview] = useState(false)
  const [reviewed, setReviewed] = useState<Set<string>>(() => new Set())
  const [boundaryError, setBoundaryError] = useState<string>()
  const [recoveryNotice, setRecoveryNotice] = useState<string>()
  const retainedCommand = useRef<{ signature: string; key: string } | undefined>(undefined)

  const attendanceQuery = useQuery({
    queryKey: attendanceKeys.detail(tutorId, sessionId ?? ''),
    queryFn: ({ signal }) => readAttendance(sessionId ?? '', signal),
    enabled: open && sessionId !== undefined,
  })
  const sheet = attendanceQuery.data

  useEffect(() => {
    if (!open) {
      setBaselineRevision(undefined)
      setDraft({})
      setReviewed(new Set())
      setRequiresReview(false)
      setBoundaryError(undefined)
      setRecoveryNotice(undefined)
      retainedCommand.current = undefined
      return
    }
    if (!sheet || baselineRevision !== undefined) return
    setBaselineRevision(sheet.revision)
    setDraft(
      Object.fromEntries(
        sheet.students.map((student) => [student.student_id, student.state ?? undefined]),
      ),
    )
  }, [baselineRevision, open, sheet])

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!sheet || !sessionId || !baselineRevision) throw new Error('Attendance is not ready yet.')
      const marks = sheet.students.map((student) => ({
        student_id: student.student_id,
        state: draft[student.student_id],
      }))
      if (marks.some((mark) => mark.state === undefined)) {
        throw new Error('Mark every student Present or Absent before saving.')
      }
      if (requiresReview && reviewed.size !== sheet.students.length) {
        throw new Error('Review every student after the stale attendance refresh.')
      }
      const input = {
        revision: baselineRevision,
        marks: marks.map((mark) => ({
          student_id: mark.student_id,
          state: mark.state as AttendanceState,
        })),
      }
      const signature = JSON.stringify(input)
      if (retainedCommand.current?.signature !== signature) {
        retainedCommand.current = { signature, key: crypto.randomUUID() }
      }
      return saveAttendance(sessionId, input, retainedCommand.current.key)
    },
    onSuccess: async () => {
      retainedCommand.current = undefined
      await queryClient.invalidateQueries({ queryKey: attendanceKeys.all })
      onSaved()
      onOpenChange(false)
    },
    onError: async (error) => {
      if (error instanceof TeachingApiError && error.body.error.code === 'attendance_changed') {
        retainedCommand.current = undefined
        const previousIDs = new Set(Object.keys(draft))
        const refreshed = await attendanceQuery.refetch()
        const fresh = refreshed.data
        if (!fresh) return
        const freshIDs = new Set(fresh.students.map((student) => student.student_id))
        const removed = [...previousIDs].filter((studentId) => !freshIDs.has(studentId)).length
        setDraft((current) =>
          Object.fromEntries(
            fresh.students.map((student) => [student.student_id, current[student.student_id]]),
          ),
        )
        setBaselineRevision(fresh.revision)
        setRequiresReview(true)
        setReviewed(new Set())
        setRecoveryNotice(
          removed > 0
            ? `${removed} student ${removed === 1 ? 'was' : 'were'} removed from this roster. Review every remaining row.`
            : 'The roster or saved attendance changed. Review every row before saving again.',
        )
        setBoundaryError(undefined)
        return
      }
      setBoundaryError(error instanceof Error ? error.message : 'Attendance could not be saved.')
    },
  })

  function mark(studentId: string, state: AttendanceState) {
    setDraft((current) => ({ ...current, [studentId]: state }))
    setReviewed((current) => new Set(current).add(studentId))
    setBoundaryError(undefined)
    retainedCommand.current = undefined
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="end"
        closeLabel="Close attendance"
        className="w-full sm:max-w-xl"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          returnFocusRef.current?.focus()
        }}
      >
        <SheetHeader>
          <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
            Whole roster pass
          </p>
          <SheetTitle>{sheet?.session.class_name ?? 'Attendance'}</SheetTitle>
          <SheetDescription>
            Mark every student once, then save the complete pass with one shared timestamp.
          </SheetDescription>
        </SheetHeader>

        {attendanceQuery.isPending ? (
          <div
            role="status"
            aria-label="Loading attendance"
            className="grid gap-3"
            aria-busy="true"
          >
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : attendanceQuery.error ? (
          <div
            role="alert"
            className="grid gap-3 rounded-lg border border-destructive/35 bg-destructive-surface p-4 text-sm text-destructive-foreground"
          >
            <p>{attendanceQuery.error.message}</p>
            <Button variant="secondary" onClick={() => void attendanceQuery.refetch()}>
              Try attendance again
            </Button>
          </div>
        ) : sheet ? (
          <>
            {!sheet.eligible && (
              <p
                role="alert"
                className="rounded-lg border border-warning/40 bg-warning-surface p-4 text-sm text-warning-foreground"
              >
                Attendance is not available because this session is{' '}
                {sheet.ineligible_reason?.replaceAll('_', ' ')}.
              </p>
            )}

            {sheet.students.length === 0 ? (
              <div className="grid gap-3 rounded-xl border border-dashed border-border-strong bg-muted/55 p-5 text-sm text-muted-foreground">
                <p>This session has no students on its dated roster.</p>
                <Button asChild variant="secondary" className="w-fit">
                  <Link to="/classes/$classId" params={{ classId: sheet.session.class_id }}>
                    Manage class roster
                  </Link>
                </Button>
              </div>
            ) : (
              <>
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => {
                    setDraft(
                      Object.fromEntries(
                        sheet.students.map((student) => [student.student_id, 'Present']),
                      ),
                    )
                    setReviewed(new Set(sheet.students.map((student) => student.student_id)))
                    retainedCommand.current = undefined
                  }}
                >
                  <CheckCheck aria-hidden="true" className="size-icon-sm" />
                  Mark all present
                </Button>

                <div className="grid gap-3">
                  {sheet.students.map((student) => (
                    <fieldset
                      key={student.student_id}
                      className="grid gap-3 rounded-lg border border-border bg-surface p-4"
                    >
                      <legend className="px-1 font-medium">
                        {student.name}
                        {student.archived && (
                          <Badge variant="warning" className="ms-2">
                            Archived
                          </Badge>
                        )}
                      </legend>
                      <RadioGroup
                        value={draft[student.student_id] ?? ''}
                        onValueChange={(value) =>
                          mark(student.student_id, value as AttendanceState)
                        }
                        className="grid grid-cols-2 gap-3"
                      >
                        {(['Present', 'Absent'] as const).map((state) => (
                          <label
                            key={state}
                            htmlFor={`attendance-${student.student_id}-${state}`}
                            className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg border border-border px-3 focus-within:ring-2 focus-within:ring-focus"
                          >
                            <RadioGroupItem
                              id={`attendance-${student.student_id}-${state}`}
                              value={state}
                            />
                            {state}
                          </label>
                        ))}
                      </RadioGroup>
                      {requiresReview && !reviewed.has(student.student_id) && (
                        <Button
                          type="button"
                          variant="quiet"
                          className="w-fit"
                          onClick={() =>
                            setReviewed((current) => new Set(current).add(student.student_id))
                          }
                        >
                          Confirm {student.name}
                        </Button>
                      )}
                      {requiresReview && reviewed.has(student.student_id) && (
                        <Badge className="w-fit">Reviewed</Badge>
                      )}
                    </fieldset>
                  ))}
                </div>
              </>
            )}
          </>
        ) : null}

        {recoveryNotice && (
          <p
            role="alert"
            className="rounded-lg border border-warning/40 bg-warning-surface p-4 text-sm text-warning-foreground"
          >
            {recoveryNotice}
          </p>
        )}
        {boundaryError && (
          <p role="alert" className="text-sm font-medium text-destructive">
            {boundaryError}
          </p>
        )}

        {sheet && sheet.students.length > 0 && (
          <SheetFooter>
            <Button variant="secondary" onClick={() => onOpenChange(false)}>
              Keep current attendance
            </Button>
            <Button
              loading={saveMutation.isPending}
              disabled={!sheet.eligible}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? 'Saving attendance…' : 'Save attendance'}
            </Button>
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  )
}
