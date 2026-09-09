import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

const alertVariants = cva('relative grid gap-1 rounded-lg border p-4 text-sm', {
  variants: {
    variant: {
      neutral: 'border-border bg-muted text-foreground',
      success: 'border-success/35 bg-success-surface text-success-foreground',
      warning: 'border-warning/35 bg-warning-surface text-warning-foreground',
      destructive: 'border-destructive/35 bg-destructive-surface text-destructive-foreground',
    },
  },
  defaultVariants: { variant: 'neutral' },
})

export type AlertProps = HTMLAttributes<HTMLDivElement> & VariantProps<typeof alertVariants>

export function Alert({ className, variant, ...props }: AlertProps) {
  return <div className={cn(alertVariants({ variant }), className)} {...props} />
}

export function AlertTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h5 className={cn('font-semibold', className)} {...props} />
}

export function AlertDescription({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('leading-relaxed', className)} {...props} />
}
