import type { ReactNode } from 'react'

type FormControlAccessibility = {
  id: string
  'aria-describedby'?: string
  'aria-invalid'?: true
}

export type FormFieldProps = {
  controlId: string
  label: string
  hint?: string
  error?: string
  requiredText?: string
  control: (accessibility: FormControlAccessibility) => ReactNode
}

/** Binds a persistent label, help, and recovery message to a caller supplied control. */
export function FormField({
  controlId,
  label,
  hint,
  error,
  requiredText,
  control,
}: FormFieldProps) {
  const hintId = hint ? `${controlId}-hint` : undefined
  const errorId = error ? `${controlId}-error` : undefined
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined

  return (
    <div className="grid gap-2">
      <label htmlFor={controlId} className="text-sm font-medium text-foreground">
        {label}
        {requiredText && <span className="ms-1 text-muted-foreground">{requiredText}</span>}
      </label>
      {control({
        id: controlId,
        'aria-describedby': describedBy,
        'aria-invalid': error ? true : undefined,
      })}
      {hint && (
        <p id={hintId} className="text-sm leading-relaxed text-muted-foreground">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} role="alert" className="text-sm font-medium text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
