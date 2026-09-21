import { zodResolver } from '@hookform/resolvers/zod'
import { type RefObject, useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import type { CreateStudentInput, Student } from '../api/teaching'
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

const studentSchema = z.object({
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

type StudentValues = z.infer<typeof studentSchema>

export type StudentFormSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreate: (input: CreateStudentInput, key: string) => Promise<Student>
  onCreated: (student: Student) => void
  returnFocusRef: RefObject<HTMLButtonElement | null>
}

/** Creates one student while retaining the command key across safe retries. */
export function StudentFormSheet({
  open,
  onOpenChange,
  onCreate,
  onCreated,
  returnFocusRef,
}: StudentFormSheetProps) {
  const [boundaryError, setBoundaryError] = useState<string>()
  const retainedCommand = useRef<{ signature: string; key: string } | undefined>(undefined)
  const form = useForm<StudentValues>({
    resolver: zodResolver(studentSchema),
    defaultValues: { name: '', phone: '' },
    mode: 'onTouched',
  })

  useEffect(() => {
    if (!open) return
    setBoundaryError(undefined)
    window.setTimeout(() => form.setFocus('name'), 0)
  }, [form, open])

  async function submit(values: StudentValues) {
    const input = {
      name: values.name.trim(),
      phone: values.phone.trim() || null,
    } satisfies CreateStudentInput
    const signature = JSON.stringify(input)
    if (retainedCommand.current?.signature !== signature) {
      retainedCommand.current = { signature, key: crypto.randomUUID() }
    }
    setBoundaryError(undefined)
    try {
      const student = await onCreate(input, retainedCommand.current.key)
      retainedCommand.current = undefined
      form.reset()
      onOpenChange(false)
      onCreated(student)
    } catch (error) {
      setBoundaryError(error instanceof Error ? error.message : 'The student could not be created.')
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="end"
        closeLabel="Close student creation"
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
          <SheetTitle>Create a student</SheetTitle>
          <SheetDescription>
            Keep the contact record simple now. You can place this student into one or more classes
            afterward.
          </SheetDescription>
        </SheetHeader>

        <form className="grid gap-5" onSubmit={form.handleSubmit(submit)} noValidate>
          <FormField
            controlId="student-name"
            label="Student name"
            requiredText="Required"
            error={form.formState.errors.name?.message}
            control={(accessibility) => (
              <Input
                {...accessibility}
                autoComplete="name"
                maxLength={160}
                {...form.register('name')}
              />
            )}
          />
          <FormField
            controlId="student-phone"
            label="Phone"
            hint="Optional. Punctuation is kept exactly as entered."
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
            <p role="alert" className="text-sm font-medium text-destructive">
              {boundaryError}
            </p>
          )}

          <SheetFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" loading={form.formState.isSubmitting}>
              {form.formState.isSubmitting ? 'Creating student…' : 'Create student'}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}
