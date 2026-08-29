import { CircleAlert } from 'lucide-react'
import type { ReactNode } from 'react'

import { cn } from '../lib/utils'

export type ErrorStateProps = {
  title: string
  description: string
  action?: ReactNode
  className?: string
  headingLevel?: 'h2' | 'h3'
}

/** Gives a blocking failure a direct recovery path and an assertive announcement. */
export function ErrorState({
  title,
  description,
  action,
  className,
  headingLevel = 'h3',
}: ErrorStateProps) {
  const Heading = headingLevel
  return (
    <section
      role="alert"
      className={cn(
        'grid gap-4 rounded-xl border border-destructive/35 bg-destructive-surface p-5 text-destructive-foreground',
        className,
      )}
    >
      <div className="flex items-start gap-3">
        <CircleAlert aria-hidden="true" className="mt-0.5 size-icon-md shrink-0" />
        <div className="grid gap-1">
          <Heading className="font-semibold">{title}</Heading>
          <p className="text-sm leading-relaxed">{description}</p>
        </div>
      </div>
      {action && <div className="ps-8">{action}</div>}
    </section>
  )
}
