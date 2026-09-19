import { Clock3, Repeat2 } from 'lucide-react'

import type { ScheduleSession } from '../api/teaching'
import { Badge } from './ui/badge'

export type ScheduleSessionCardProps = {
  session: ScheduleSession
}

/** Shows one concrete session without making its class color carry meaning alone. */
export function ScheduleSessionCard({ session }: ScheduleSessionCardProps) {
  return (
    <article
      data-class-color={session.class_color}
      className="grid gap-2 rounded-lg border border-class-border border-s-4 border-s-class-marker bg-class-surface p-3 shadow-field"
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <h3 className="text-sm font-semibold text-foreground">{session.class_name}</h3>
        <Badge className="bg-surface text-foreground">{session.state}</Badge>
      </div>
      <p className="flex items-center gap-2 font-mono text-sm text-foreground">
        <Clock3 aria-hidden="true" className="size-icon-sm text-class-marker" />
        <time dateTime={session.starts_at}>{session.display_start}</time>
        <span aria-hidden="true">–</span>
        <time dateTime={session.ends_at}>{session.display_end}</time>
      </p>
      {session.source_time_zone && (
        <p className="flex items-center gap-2 text-caption text-muted-foreground">
          <Repeat2 aria-hidden="true" className="size-icon-sm" />
          Weekly rule in {session.source_time_zone}
        </p>
      )}
    </article>
  )
}
