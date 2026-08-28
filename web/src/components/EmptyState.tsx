import type { ReactNode } from 'react'

import { cn } from '../lib/utils'

export type EmptyStateProps = {
  icon?: ReactNode
  title: string
  description: string
  action?: ReactNode
  className?: string
}

/** Turns empty space into a clear explanation and an optional next action. */
export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <section
      className={cn(
        'grid justify-items-center gap-3 rounded-xl border border-dashed border-border-strong bg-muted/55 px-5 py-10 text-center',
        className,
      )}
    >
      {icon && (
        <div
          aria-hidden="true"
          className="grid size-12 place-items-center rounded-full bg-surface text-primary shadow-field"
        >
          {icon}
        </div>
      )}
      <div className="grid max-w-md gap-1">
        <h3 className="text-base font-semibold">{title}</h3>
        <p className="text-sm leading-relaxed text-muted-foreground">{description}</p>
      </div>
      {action}
    </section>
  )
}
