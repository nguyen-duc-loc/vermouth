import type { ClassColor } from '../design-system/class-colors'
import { cn } from '../lib/utils'

export type ClassColorCardProps = {
  color: ClassColor
  title: string
  detail?: string
  className?: string
}

/** Carries class identity through a stable surface and edge marker plus readable text. */
export function ClassColorCard({ color, title, detail, className }: ClassColorCardProps) {
  return (
    <article
      data-class-color={color}
      className={cn(
        'class-color-surface relative overflow-hidden rounded-lg border border-class-border bg-class-surface p-4 ps-5 text-foreground',
        className,
      )}
    >
      <span aria-hidden="true" className="absolute inset-y-0 start-0 w-1.5 bg-class-marker" />
      <h3 className="text-sm font-semibold">{title}</h3>
      {detail && <p className="mt-1 text-sm text-muted-foreground">{detail}</p>}
    </article>
  )
}
