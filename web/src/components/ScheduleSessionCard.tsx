import { Clock3, Move, Repeat2 } from 'lucide-react'

import type { ScheduleSession } from '../api/teaching'
import { Badge } from './ui/badge'

export type ScheduleSessionCardProps = {
  session: ScheduleSession
  onSelect: (session: ScheduleSession, trigger: HTMLButtonElement) => void
}

/** Shows one concrete session without making its class color carry meaning alone. */
export function ScheduleSessionCard({ session, onSelect }: ScheduleSessionCardProps) {
  return (
    <button
      type="button"
      onClick={(event) => onSelect(session, event.currentTarget)}
      aria-label={`Open ${session.class_name} session at ${session.display_start}`}
      data-class-color={session.class_color}
      className="grid min-h-11 min-w-0 w-full gap-2 rounded-lg border border-class-border border-s-4 border-s-class-marker bg-class-surface p-3 text-start shadow-field outline-none transition-[border-color,box-shadow,transform] duration-base ease-standard motion-reduce:transition-none hover:border-class-marker focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background active:translate-y-px"
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <h3 className="min-w-0 text-sm font-semibold text-foreground wrap-anywhere">
          {session.class_name}
        </h3>
        <div className="flex flex-wrap justify-end gap-1">
          <Badge className="bg-surface text-foreground">{session.state}</Badge>
          {session.moved_at && (
            <Badge className="gap-1 bg-surface text-foreground">
              <Move aria-hidden="true" className="size-icon-sm" />
              moved
            </Badge>
          )}
        </div>
      </div>
      <p className="flex min-w-0 flex-wrap items-center gap-2 font-mono text-sm text-foreground">
        <Clock3 aria-hidden="true" className="size-icon-sm shrink-0 text-class-marker" />
        <time dateTime={session.starts_at}>{session.display_start}</time>
        <span aria-hidden="true">–</span>
        <time dateTime={session.ends_at}>{session.display_end}</time>
      </p>
      {session.source_time_zone && (
        <p className="flex min-w-0 flex-wrap items-center gap-2 text-caption text-muted-foreground wrap-anywhere">
          <Repeat2 aria-hidden="true" className="size-icon-sm shrink-0" />
          Weekly rule in {session.source_time_zone}
        </p>
      )}
    </button>
  )
}
