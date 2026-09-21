import { zodResolver } from '@hookform/resolvers/zod'
import { type RefObject, useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import { type Student, TeachingApiError, type UpdateStudentInput } from '../api/teaching'
import { FormField } from './FormField'
import { Button } from './ui/button'
import { Input } from './ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from './ui/sheet'

const editStudentSchema = z.object({
  name: z.string().superRefine((value, context) => {
    const length = [...value.trim()].length
    if (length < 1 || length > 160) {
      context.addIssue({ code: 'custom', message: 'Use 1 through 160 characters.' })
    }
  }),
  phone: z.string().superRefine((value, context) => {
    if ([...value.trim()].length > 40) {
      context.addIssue({ code: 'custom', message: 'Use at most 40 characters.' })
    }
  }),
})

type EditStudentValues = z.infer<typeof editStudentSchema>

export type StudentEditSheetProps = {
  open: boolean
  student: Student
  onOpenChange: (open: boolean) => void
  onUpdate: (input: UpdateStudentInput, key: string) => Promise<Student>
  onReadCurrent: () => Promise<Student>
  onUpdated: (student: Student) => void
  returnFocusRef: RefObject<HTMLButtonElement | null>
}

/** Edits one student without merging a stale local draft into newer server truth. */
export function StudentEditSheet({
  open,
  student,
  onOpenChange,
  onUpdate,
  onReadCurrent,
  onUpdated,
  returnFocusRef,
}: StudentEditSheetProps) {
  const [boundaryError, setBoundaryError] = useState<string>()
  const [currentRecord, setCurrentRecord] = useState<Student>()
  const [baseline, setBaseline] = useState(student)
  const retainedCommand = useRef<{ signature: string; key: string } | undefined>(undefined)
  const form = useForm<EditStudentValues>({
    resolver: zodResolver(editStudentSchema),
    defaultValues: { name: student.name, phone: student.phone ?? '' },
    mode: 'onTouched',
  })

  useEffect(() => {
    if (!open) return
    form.reset({ name: student.name, phone: student.phone ?? '' })
    setBaseline(student)
    setBoundaryError(undefined)
    setCurrentRecord(undefined)
    retainedCommand.current = undefined
    window.setTimeout(() => form.setFocus('name'), 0)
  }, [form, open, student])

  async function submit(values: EditStudentValues) {
    const input = {
      expected_updated_at: baseline.updated_at,
      name: values.name.trim(),
      phone: values.phone.trim() || null,
    } satisfies UpdateStudentInput
    const signature = JSON.stringify(input)
    if (retainedCommand.current?.signature !== signature) {
      retainedCommand.current = { signature, key: crypto.randomUUID() }
    }
    setBoundaryError(undefined)
    setCurrentRecord(undefined)
    try {
      const updated = await onUpdate(input, retainedCommand.current.key)
      retainedCommand.current = undefined
      onOpenChange(false)
      onUpdated(updated)
    } catch (error) {
      if (error instanceof TeachingApiError && error.body.error.code === 'student_changed') {
        const current = await onReadCurrent()
        setCurrentRecord(current)
        setBoundaryError('This record changed elsewhere. Review both versions before trying again.')
        retainedCommand.current = undefined
        return
      }
      setBoundaryError(error instanceof Error ? error.message : 'The student could not be updated.')
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="end"
        closeLabel="Close student edit"
        className="w-full sm:max-w-lg"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          returnFocusRef.current?.focus()
        }}
      >
        <SheetHeader>
          <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
            Student record
          </p>
          <SheetTitle>Edit {student.name}</SheetTitle>
          <SheetDescription>
            Name changes reach teaching projections. Phone changes stay private to teaching.
          </SheetDescription>
        </SheetHeader>

        <form className="grid gap-5" onSubmit={form.handleSubmit(submit)} noValidate>
          <FormField
            controlId="edit-student-name"
            label="Student name"
            requiredText="Required"
            error={form.formState.errors.name?.message}
            control={(accessibility) => (
              <Input {...accessibility} maxLength={160} {...form.register('name')} />
            )}
          />
          <FormField
            controlId="edit-student-phone"
            label="Phone"
            hint="Leave this empty to clear the saved phone."
            error={form.formState.errors.phone?.message}
            control={(accessibility) => (
              <Input
                {...accessibility}
                type="tel"
                autoComplete="tel"
                maxLength={40}
                {...form.register('phone')}
              />
            )}
          />

          {boundaryError && (
            <div
              role="alert"
              className="grid gap-2 rounded-lg border border-destructive/35 bg-destructive-surface p-4 text-sm text-destructive-foreground"
            >
              <p className="font-medium">{boundaryError}</p>
              {currentRecord && (
                <div className="grid gap-1">
                  <p>Current name: {currentRecord.name}</p>
                  <p>Current phone: {currentRecord.phone ?? 'Not saved'}</p>
                  <Button
                    type="button"
                    variant="secondary"
                    className="mt-2 w-fit"
                    onClick={() => {
                      form.reset({ name: currentRecord.name, phone: currentRecord.phone ?? '' })
                      setBaseline(currentRecord)
                      setBoundaryError(undefined)
                      setCurrentRecord(undefined)
                    }}
                  >
                    Use current record
                  </Button>
                </div>
              )}
            </div>
          )}

          <SheetFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              loading={form.formState.isSubmitting}
              disabled={currentRecord !== undefined}
            >
              {form.formState.isSubmitting ? 'Saving changes…' : 'Save changes'}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}
