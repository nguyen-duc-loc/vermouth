import { zodResolver } from '@hookform/resolvers/zod'
import { ArrowRight, BookOpenCheck, CalendarClock, UserRoundPlus } from 'lucide-react'
import { type RefObject, useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import type {
  CreateClassInput,
  CreateClassResult,
  CreateStudentInput,
  JoinRosterInput,
  RosterPeriod,
  SetupDefaults,
  Student,
} from '../api/teaching'
import { CLASS_COLOR_LABELS, CLASS_COLORS } from '../design-system/class-colors'
import {
  clearTeachingDraft,
  loadTeachingDraft,
  newTeachingDraft,
  saveTeachingDraft,
  type TeachingDraft,
  type TeachingDraftValues,
} from '../lib/teaching-draft'
import { FormField } from './FormField'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { RadioGroup, RadioGroupItem } from './ui/radio-group'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from './ui/sheet'

const localTime = /^(?:[01][0-9]|2[0-3]):[0-5][0-9]$/
const localDate = /^\d{4}-\d{2}-\d{2}$/

const setupSchema = z
  .object({
    className: z.string(),
    rateAmount: z.string(),
    color: z.enum(['suggest', 'red', 'rose', 'orange', 'green', 'blue', 'yellow', 'violet']),
    localDate: z.string().regex(localDate, 'Choose a session date.'),
    startTime: z.string().regex(localTime, 'Choose a start time.'),
    endTime: z.string().regex(localTime, 'Choose an end time.'),
    studentName: z.string(),
    phone: z.string(),
  })
  .superRefine((values, context) => {
    const classLength = [...values.className.trim()].length
    if (classLength < 1 || classLength > 120) {
      context.addIssue({
        code: 'custom',
        path: ['className'],
        message: 'Use 1 through 120 characters.',
      })
    }
    if (!/^\d+$/.test(values.rateAmount)) {
      context.addIssue({ code: 'custom', path: ['rateAmount'], message: 'Enter whole dong only.' })
    } else if (Number(values.rateAmount) > 1_000_000_000) {
      context.addIssue({
        code: 'custom',
        path: ['rateAmount'],
        message: 'Use at most 1,000,000,000 dong.',
      })
    }
    if (values.endTime <= values.startTime) {
      context.addIssue({
        code: 'custom',
        path: ['endTime'],
        message: 'End time must be later than start time.',
      })
    }
    const studentLength = [...values.studentName.trim()].length
    if (studentLength < 1 || studentLength > 120) {
      context.addIssue({
        code: 'custom',
        path: ['studentName'],
        message: 'Use 1 through 120 characters.',
      })
    }
    if ([...values.phone].length > 40) {
      context.addIssue({ code: 'custom', path: ['phone'], message: 'Use at most 40 characters.' })
    }
  })

type SetupValues = z.infer<typeof setupSchema>

export type TeachingSetupSheetProps = {
  open: boolean
  tutorId: string
  defaults: SetupDefaults
  onOpenChange: (open: boolean) => void
  onCreateClass: (input: CreateClassInput, key: string) => Promise<CreateClassResult>
  onCreateStudent: (input: CreateStudentInput, key: string) => Promise<Student>
  onJoinRoster: (classId: string, input: JoinRosterInput) => Promise<RosterPeriod>
  onComplete: (localDate: string) => void
  returnFocusRef: RefObject<HTMLButtonElement | null>
}

const steps = [
  { key: 'class', label: 'Class and session', icon: CalendarClock },
  { key: 'student', label: 'Student', icon: UserRoundPlus },
  { key: 'roster', label: 'Roster', icon: BookOpenCheck },
] as const

/** Guides one resumable class setup while each completed server fact stays committed. */
export function TeachingSetupSheet({
  open,
  tutorId,
  defaults,
  onOpenChange,
  onCreateClass,
  onCreateStudent,
  onJoinRoster,
  onComplete,
  returnFocusRef,
}: TeachingSetupSheetProps) {
  const [draft, setDraft] = useState<TeachingDraft>(
    () => loadTeachingDraft(tutorId) ?? newTeachingDraft(tutorId, defaults),
  )
  const [boundaryError, setBoundaryError] = useState<string>()
  const [submitting, setSubmitting] = useState(false)
  const form = useForm<SetupValues>({
    resolver: zodResolver(setupSchema),
    defaultValues: draft.values,
    mode: 'onTouched',
  })

  useEffect(() => {
    if (!open) return
    const recovered = loadTeachingDraft(tutorId) ?? newTeachingDraft(tutorId, defaults)
    setDraft(recovered)
    form.reset(recovered.values)
    setBoundaryError(undefined)
  }, [defaults, form, open, tutorId])

  useEffect(() => {
    const subscription = form.watch((values) => {
      setDraft((current) => {
        const next = {
          ...current,
          values: { ...current.values, ...values } as TeachingDraftValues,
        }
        saveTeachingDraft(next)
        return next
      })
    })
    return () => subscription.unsubscribe()
  }, [form])

  async function submitStep() {
    setBoundaryError(undefined)
    setSubmitting(true)
    try {
      const values = form.getValues()
      if (draft.step === 'class') {
        const valid = await form.trigger([
          'className',
          'rateAmount',
          'color',
          'localDate',
          'startTime',
          'endTime',
        ])
        if (!valid) return
        const result = await onCreateClass(
          {
            name: values.className,
            color: values.color === 'suggest' ? null : values.color,
            rate_amount: Number(values.rateAmount),
            first_session: {
              local_date: values.localDate,
              start_time: values.startTime,
              end_time: values.endTime,
            },
          },
          draft.classKey,
        )
        const next: TeachingDraft = {
          ...draft,
          step: 'student',
          classId: result.class.class_id,
          sessionId: result.first_session.session_id,
          firstLocalDate: result.first_session.local_date,
          values,
        }
        saveTeachingDraft(next)
        setDraft(next)
        form.setFocus('studentName')
        return
      }
      if (draft.step === 'student') {
        const valid = await form.trigger(['studentName', 'phone'])
        if (!valid) return
        const result = await onCreateStudent(
          { name: values.studentName, phone: values.phone === '' ? null : values.phone },
          draft.studentKey,
        )
        const next: TeachingDraft = {
          ...draft,
          step: 'roster',
          studentId: result.student_id,
          values,
        }
        saveTeachingDraft(next)
        setDraft(next)
        return
      }
      if (!draft.classId || !draft.studentId || !draft.firstLocalDate) {
        throw new Error(
          'The saved setup identifiers are incomplete. Close this sheet and start again.',
        )
      }
      await onJoinRoster(draft.classId, {
        student_id: draft.studentId,
        effective_from: draft.firstLocalDate,
      })
      clearTeachingDraft(tutorId)
      onComplete(draft.firstLocalDate)
      onOpenChange(false)
    } catch (error) {
      setBoundaryError(
        error instanceof Error ? error.message : 'This setup step could not be saved.',
      )
    } finally {
      setSubmitting(false)
    }
  }

  const stepIndex = steps.findIndex((step) => step.key === draft.step)
  const actionLabel =
    draft.step === 'class'
      ? 'Create class and session'
      : draft.step === 'student'
        ? 'Create student'
        : 'Complete roster'

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="end"
        closeLabel="Close class setup"
        className="w-full sm:max-w-xl"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          returnFocusRef.current?.focus()
        }}
      >
        <SheetHeader>
          <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
            Step {stepIndex + 1} of {steps.length}
          </p>
          <SheetTitle>Set up your teaching day</SheetTitle>
          <SheetDescription>
            Each finished step is saved. If the connection stops, you can reopen this sheet and
            continue.
          </SheetDescription>
        </SheetHeader>

        <ol aria-label="Setup progress" className="grid grid-cols-3 gap-2">
          {steps.map((step, index) => {
            const Icon = step.icon
            const current = index === stepIndex
            const complete = index < stepIndex
            return (
              <li
                key={step.key}
                aria-current={current ? 'step' : undefined}
                className="grid min-w-0 gap-2 rounded-lg border border-border bg-muted p-3 text-xs text-muted-foreground"
              >
                <Icon aria-hidden="true" className="size-icon-sm text-primary" />
                <span className="wrap-anywhere">{step.label}</span>
                <span className="font-medium text-foreground">
                  {complete ? 'Saved' : current ? 'Current' : 'Next'}
                </span>
              </li>
            )
          })}
        </ol>

        <form
          className="grid gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            void submitStep()
          }}
        >
          {draft.step === 'class' && (
            <>
              <FormField
                controlId="setup-class-name"
                label="Class name"
                error={form.formState.errors.className?.message}
                control={(accessibility) => (
                  <Input {...accessibility} {...form.register('className')} autoComplete="off" />
                )}
              />
              <FormField
                controlId="setup-rate"
                label="Rate per present session"
                hint="Whole Vietnamese dong, from 0 through 1,000,000,000."
                error={form.formState.errors.rateAmount?.message}
                control={(accessibility) => (
                  <Input
                    {...accessibility}
                    {...form.register('rateAmount')}
                    inputMode="numeric"
                    autoComplete="off"
                  />
                )}
              />
              <fieldset className="grid gap-3">
                <legend className="text-sm font-medium text-foreground">Class color</legend>
                <RadioGroup
                  value={form.watch('color')}
                  onValueChange={(value) =>
                    form.setValue('color', value as SetupValues['color'], { shouldDirty: true })
                  }
                  className="grid gap-2 sm:grid-cols-2"
                >
                  <RadioGroupItem
                    value="suggest"
                    aria-label="Suggest for me"
                    className="flex size-auto min-h-11 w-full justify-start gap-3 rounded-lg bg-surface px-3 py-2 text-sm font-medium text-foreground"
                  >
                    Suggest for me
                  </RadioGroupItem>
                  {CLASS_COLORS.map((color) => (
                    <RadioGroupItem
                      key={color}
                      value={color}
                      aria-label={CLASS_COLOR_LABELS[color].en}
                      data-class-color={color}
                      className="flex size-auto min-h-11 w-full justify-start gap-3 rounded-lg border-class-border bg-class-surface px-3 py-2 text-sm font-medium text-foreground"
                    >
                      {CLASS_COLOR_LABELS[color].en}
                    </RadioGroupItem>
                  ))}
                </RadioGroup>
              </fieldset>
              <div className="grid gap-4 rounded-xl border border-border bg-muted p-4 sm:grid-cols-3">
                <FormField
                  controlId="setup-local-date"
                  label="Session date"
                  error={form.formState.errors.localDate?.message}
                  control={(accessibility) => (
                    <Input {...accessibility} {...form.register('localDate')} type="date" />
                  )}
                />
                <FormField
                  controlId="setup-start-time"
                  label="Starts"
                  error={form.formState.errors.startTime?.message}
                  control={(accessibility) => (
                    <Input {...accessibility} {...form.register('startTime')} type="time" />
                  )}
                />
                <FormField
                  controlId="setup-end-time"
                  label="Ends"
                  error={form.formState.errors.endTime?.message}
                  control={(accessibility) => (
                    <Input {...accessibility} {...form.register('endTime')} type="time" />
                  )}
                />
              </div>
            </>
          )}

          {draft.step === 'student' && (
            <>
              <div className="rounded-xl border border-success/35 bg-success-surface p-4 text-sm leading-relaxed text-success-foreground">
                Class and first session saved. Add the first student now.
              </div>
              <FormField
                controlId="setup-student-name"
                label="Student name"
                error={form.formState.errors.studentName?.message}
                control={(accessibility) => (
                  <Input {...accessibility} {...form.register('studentName')} autoComplete="off" />
                )}
              />
              <FormField
                controlId="setup-phone"
                label="Phone"
                hint="Optional. It stays in teaching and never appears on home or in an event."
                error={form.formState.errors.phone?.message}
                control={(accessibility) => (
                  <Input
                    {...accessibility}
                    {...form.register('phone')}
                    type="tel"
                    autoComplete="tel"
                  />
                )}
              />
            </>
          )}

          {draft.step === 'roster' && (
            <div className="grid gap-3 rounded-xl border border-success/35 bg-success-surface p-5 text-success-foreground">
              <BookOpenCheck aria-hidden="true" className="size-icon-lg" />
              <h3 className="text-lg font-semibold">Class and student saved</h3>
              <p className="text-sm leading-relaxed">
                Complete the roster to include this student from {draft.firstLocalDate}.
              </p>
            </div>
          )}

          {boundaryError && (
            <div
              role="alert"
              className="rounded-lg border border-destructive/35 bg-destructive-surface p-4 text-sm font-medium text-destructive-foreground"
            >
              {boundaryError} Your entries and this step are still saved.
            </div>
          )}

          <SheetFooter>
            <Button type="submit" loading={submitting} className="w-full sm:w-auto sm:ms-auto">
              {actionLabel}
              <ArrowRight aria-hidden="true" className="size-icon-sm" />
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}
