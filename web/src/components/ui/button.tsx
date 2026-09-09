import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { LoaderCircle } from 'lucide-react'
import type { ButtonHTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

const buttonVariants = cva(
  'inline-flex min-h-11 items-center justify-center gap-2 rounded-md px-4 text-sm font-medium whitespace-normal transition-[color,background-color,border-color,box-shadow,transform] duration-base ease-standard outline-none motion-reduce:transition-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 active:translate-y-px',
  {
    variants: {
      variant: {
        primary:
          'border border-primary bg-primary text-primary-foreground hover:bg-primary-emphasis',
        secondary:
          'border border-border-strong bg-surface text-foreground hover:border-primary hover:text-primary',
        quiet: 'border border-transparent bg-transparent text-foreground hover:bg-muted',
        destructive:
          'border border-destructive bg-destructive text-destructive-contrast hover:bg-destructive-strong',
      },
      size: {
        default: 'min-h-11 px-4 py-2.5',
        large: 'min-h-12 px-5 py-3 text-base',
        icon: 'size-11 shrink-0 p-0',
      },
    },
    defaultVariants: {
      variant: 'primary',
      size: 'default',
    },
  },
)

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
    loading?: boolean
  }

/** Provides the shared action states without inventing any visible label. */
export function Button({
  asChild = false,
  className,
  disabled,
  loading = false,
  type = 'button',
  variant,
  size,
  children,
  ...props
}: ButtonProps) {
  const classes = cn(buttonVariants({ variant, size }), className)

  if (asChild) {
    return (
      <Slot className={classes} aria-busy={loading || undefined} {...props}>
        {children}
      </Slot>
    )
  }

  return (
    <button
      type={type}
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading && <LoaderCircle aria-hidden="true" className="size-icon-sm animate-spin" />}
      {children}
    </button>
  )
}
