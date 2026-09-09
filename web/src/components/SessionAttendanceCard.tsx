import { Clock3, UsersRound } from 'lucide-react'

import type { AttendanceState, HomeSession } from '../api/teaching'
import { Badge } from './ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { RadioGroup, RadioGroupItem } from './ui/radio-group'

export type SessionAttendanceCardProps = {
  session: HomeSession
  timeZone: string
  pendingStudentId?: string
  savedMessage?: string
  error?: string
  onMark: (studentId: string, state: AttendanceState) => void
}

/** Keeps each attendance action beside the session and student it changes. */
export function SessionAttendanceCard({
  session,
  timeZone,
  pendingStudentId,
  savedMessage,
  error,
  onMark,
}: SessionAttendanceCardProps) {
  const timeFormat = new Intl.DateTimeFormat('en', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone,
  })
  return (
    <Card
      data-class-color={session.class_color}
      className="border-s-4 border-s-class-marker bg-class-surface"
    >
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="grid gap-1">
            <CardTitle>{session.class_name}</CardTitle>
            <CardDescription className="flex flex-wrap items-center gap-2 text-foreground">
              <Clock3 aria-hidden="true" className="size-icon-sm text-class-marker" />
              <time dateTime={session.starts_at}>
                {timeFormat.format(new Date(session.starts_at))}
              </time>
              <span>to</span>
              <time dateTime={session.ends_at}>{timeFormat.format(new Date(session.ends_at))}</time>
            </CardDescription>
          </div>
          <Badge className="border-class-border bg-surface text-foreground">
            <UsersRound aria-hidden="true" className="size-icon-sm" />
            {session.students.length} {session.students.length === 1 ? 'student' : 'students'}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="grid gap-5">
        {session.students.map((student) => {
          const groupId = `attendance-${session.session_id}-${student.student_id}`
          const current = student.attendance_state ?? 'Unmarked'
          const pending = pendingStudentId === student.student_id
          return (
            <fieldset
              key={student.student_id}
              className="grid gap-3 rounded-lg border border-class-border bg-surface p-4"
            >
              <legend
                id={`${groupId}-legend`}
                className="px-1 text-sm font-semibold text-foreground"
              >
                {student.name}
              </legend>
              <RadioGroup
                aria-labelledby={`${groupId}-legend`}
                value={current}
                disabled={pending}
                onValueChange={(value) => {
                  if (value === 'Present' || value === 'Absent') onMark(student.student_id, value)
                }}
                className="grid gap-2 sm:grid-cols-3"
              >
                {(['Unmarked', 'Present', 'Absent'] as const).map((state) => (
                  <label
                    key={state}
                    htmlFor={`${groupId}-${state}`}
                    className="flex min-h-11 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2 text-sm font-medium has-[[data-state=checked]]:border-primary has-[[data-state=checked]]:bg-primary/10"
                  >
                    <RadioGroupItem
                      id={`${groupId}-${state}`}
                      value={state}
                      disabled={state === 'Unmarked'}
                    />
                    {state === 'Unmarked' ? 'Not marked' : state}
                  </label>
                ))}
              </RadioGroup>
              {pending && (
                <p role="status" className="text-sm text-muted-foreground">
                  Saving {student.name} attendance…
                </p>
              )}
            </fieldset>
          )
        })}
        {session.students.length === 0 && (
          <p className="rounded-lg border border-border bg-muted p-4 text-sm leading-relaxed text-muted-foreground">
            No covered students are in this session yet.
          </p>
        )}
        <div
          role="status"
          aria-live="polite"
          aria-atomic="true"
          className="text-sm text-success-foreground"
        >
          {savedMessage}
        </div>
        {error && (
          <div role="alert" className="text-sm font-medium text-destructive">
            {error}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
