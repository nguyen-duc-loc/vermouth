import type { InputHTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

export type InputProps = InputHTMLAttributes<HTMLInputElement>

/** Keeps text input semantics while applying the shared focus and error treatment. */
export function Input({ className, type, ...props }: InputProps) {
  return (
    <input
      type={type}
      className={cn(
        'flex min-h-11 w-full rounded-md border border-input bg-surface px-3 py-2 text-base text-foreground shadow-field outline-none transition-[border-color,box-shadow] duration-base ease-standard placeholder:text-muted-foreground motion-reduce:transition-none focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:bg-muted disabled:opacity-70 aria-invalid:border-destructive aria-invalid:ring-destructive/25 md:text-sm',
        className,
      )}
      {...props}
    />
  )
}
