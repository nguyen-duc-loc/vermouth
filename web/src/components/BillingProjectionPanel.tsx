import { CheckCircle2, Clock3, RefreshCw, TriangleAlert } from 'lucide-react'

import type { BillingProjection } from '../api/teaching'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'

export type BillingProjectionPanelProps = {
  projection?: BillingProjection | null
  unavailable?: string | null
  timedOut: boolean
  refreshing: boolean
  onRetry: () => void
}

const countLabels: ReadonlyArray<[keyof BillingProjection, string]> = [
  ['class_count', 'Classes'],
  ['session_count', 'Sessions'],
  ['student_count', 'Students'],
  ['open_roster_count', 'Open roster periods'],
  ['attendance_count', 'Attendance rows'],
]

/** Shows projection progress as secondary system context, never teaching truth. */
export function BillingProjectionPanel({
  projection,
  unavailable,
  timedOut,
  refreshing,
  onRetry,
}: BillingProjectionPanelProps) {
  const active = projection?.state === 'active'
  const statusAnnouncement = active
    ? 'Billing projection is active.'
    : unavailable
      ? 'Billing projection is unavailable. Teaching work remains available.'
      : timedOut
        ? 'Billing projection is still catching up. Manual retry is available.'
        : 'Billing projection is catching up.'
  return (
    <Card aria-labelledby="billing-projection-title">
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="grid size-11 place-items-center rounded-lg bg-primary/10 text-primary">
            {active ? (
              <CheckCircle2 aria-hidden="true" className="size-icon-md" />
            ) : unavailable ? (
              <TriangleAlert aria-hidden="true" className="size-icon-md" />
            ) : (
              <Clock3 aria-hidden="true" className="size-icon-md" />
            )}
          </div>
          <Badge variant={active ? 'success' : unavailable ? 'warning' : 'primary'}>
            {active ? 'Projection active' : unavailable ? 'Projection unavailable' : 'Catching up'}
          </Badge>
        </div>
        <CardTitle id="billing-projection-title">Billing projection</CardTitle>
        <CardDescription>
          Billing keeps rebuildable copies for future invoices. Attendance remains owned by
          teaching.
        </CardDescription>
      </CardHeader>
      <div role="status" aria-live="polite" aria-atomic="true" className="sr-only">
        {statusAnnouncement}
      </div>
      <CardContent className="grid gap-4">
        {projection && (
          <dl className="grid grid-cols-2 gap-3 text-sm">
            {countLabels.map(([field, label]) => (
              <div key={field} className="rounded-lg border border-border bg-muted p-3">
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="mt-1 font-mono text-lg font-semibold text-foreground">
                  {String(projection[field])}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {projection?.latest_updated_at && (
          <p className="text-sm leading-relaxed text-muted-foreground">
            Latest projection update{' '}
            <time
              dateTime={projection.latest_updated_at}
              className="font-mono text-xs text-foreground"
            >
              {new Intl.DateTimeFormat('en', {
                dateStyle: 'medium',
                timeStyle: 'short',
              }).format(new Date(projection.latest_updated_at))}
            </time>
          </p>
        )}
        {(unavailable || timedOut) && (
          <div className="grid gap-3 rounded-lg border border-warning/35 bg-warning-surface p-4 text-warning-foreground">
            <p className="text-sm leading-relaxed">
              {unavailable
                ? 'Billing did not answer. Your teaching work is still available.'
                : 'Billing is still catching up after ten seconds. You can check again when you are ready.'}
            </p>
            <Button variant="secondary" loading={refreshing} onClick={onRetry} className="w-fit">
              <RefreshCw aria-hidden="true" className="size-icon-sm" />
              Check billing again
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
