import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

const badgeVariants = cva(
  'inline-flex min-h-6 items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-semibold',
  {
    variants: {
      variant: {
        neutral: 'border-border bg-muted text-foreground',
        primary: 'border-primary/30 bg-primary/10 text-primary-emphasis',
        success: 'border-success/30 bg-success-surface text-success-foreground',
        warning: 'border-warning/30 bg-warning-surface text-warning-foreground',
        destructive: 'border-destructive/30 bg-destructive-surface text-destructive-foreground',
      },
    },
    defaultVariants: { variant: 'neutral' },
  },
)

export type BadgeProps = HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />
}
