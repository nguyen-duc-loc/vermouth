import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Activity, ArrowRight, BookOpen, Check, Home, LogOut, Radio, UserRound } from 'lucide-react'
import { useEffect, useState } from 'react'

import { signOut } from '../api/session'
import { readThread } from '../api/thread'
import { AppearancePanel, type AppearancePanelText } from '../components/AppearancePanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { ClassColorCard } from '../components/ClassColorCard'
import { ErrorState } from '../components/ErrorState'
import { PageEntrance } from '../components/PageEntrance'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Separator } from '../components/ui/separator'
import { Skeleton } from '../components/ui/skeleton'

// The skeleton's one screen. It exists to show the whole pipe working from a
// browser: a signed in read through the gateway, and a projection in
// notifications that caught up a moment after the write.
//
// The tutor arrives here already signed in: the route redirects to /signin
// otherwise, and the token came from the boot time refresh (spec 0004).
//
// Feature 6 owns the design system, and feature 8 replaces this screen with the
// real teaching loop. Nothing here is meant to survive that.
const appearanceText: AppearancePanelText = {
  title: 'Appearance',
  themeLegend: 'Theme',
  accentLegend: 'Accent color',
  themes: { light: 'Light', dark: 'Dark', system: 'System' },
  accents: {
    red: 'Red',
    rose: 'Rose',
    orange: 'Orange',
    green: 'Green',
    blue: 'Blue',
    yellow: 'Yellow',
    violet: 'Violet',
  },
}

const destinations: readonly AppDestination[] = [{ href: '/', label: 'Thread', icon: Home }]

export function ThreadPage() {
  const navigate = useNavigate()
  const [signingOut, setSigningOut] = useState(false)

  const thread = useQuery({
    queryKey: ['thread'],
    queryFn: readThread,
    // The relay polls, so the projection is a moment behind the write. Asking
    // again is the honest way to show that, rather than pretending it is there.
    refetchInterval: (query) => (query.state.data?.projection.recorded ? false : 500),
  })

  const recorded = thread.data?.projection.recorded ?? false

  useEffect(() => {
    document.title = 'Development thread · Vermouth'
  }, [])

  const accountPanel = (
    <div className="grid gap-6">
      <AppearancePanel text={appearanceText} />
      <Separator />
      <Button
        variant="secondary"
        loading={signingOut}
        onClick={async () => {
          setSigningOut(true)
          const session = await signOut()
          if (session.status === 'anonymous') {
            await navigate({ to: '/signin', search: { redirect: '/' } })
            return
          }
          setSigningOut(false)
        }}
      >
        <LogOut aria-hidden="true" className="size-icon-sm" />
        {signingOut ? 'Signing out…' : 'Sign out'}
      </Button>
    </div>
  )

  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to the thread',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Change this device appearance or end the current session.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={accountPanel}
      contextualPanel={
        <div className="grid gap-5">
          <div className="grid gap-2">
            <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
              Thread state
            </p>
            <Badge variant={recorded ? 'success' : 'warning'} className="w-fit">
              {recorded ? 'Projection caught up' : 'Waiting for projection'}
            </Badge>
          </div>
          <Separator />
          <ClassColorCard
            color="blue"
            title="Design signature"
            detail="Class identity stays separate from system state."
          />
        </div>
      }
    >
      <PageEntrance className="mx-auto grid w-full max-w-6xl gap-8 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
        >
          <div className="grid max-w-3xl gap-3">
            <Badge variant="primary" className="w-fit">
              Tracer bullet
            </Badge>
            <h1 className="text-3xl font-semibold text-balance">
              Watch one fact cross the whole system.
            </h1>
            <p className="text-base leading-relaxed text-muted-foreground">
              The gateway reads the tutor from identity, while notifications shows the projection it
              built after the event crossed the outbox, relay, and Redpanda.
            </p>
          </div>
          <div className="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-2 text-sm text-muted-foreground shadow-field">
            <Activity aria-hidden="true" className="size-icon-sm text-primary" />
            Live development proof
          </div>
        </header>

        {thread.error ? (
          <ErrorState
            title="The development thread could not be read"
            description={thread.error.message}
            action={
              <Button variant="secondary" onClick={() => void thread.refetch()}>
                Try the read again
              </Button>
            }
          />
        ) : (
          <section data-entrance-item aria-labelledby="thread-heading" className="grid gap-4">
            <header className="grid gap-1">
              <h2 id="thread-heading" className="text-xl font-semibold">
                Event journey
              </h2>
              <p className="text-sm text-muted-foreground">
                Each surface says which bounded context owns the fact it displays.
              </p>
            </header>

            <div aria-live="polite" className="grid gap-4 lg:grid-cols-2">
              <Card>
                <CardHeader>
                  <div className="mb-1 flex flex-wrap items-center justify-between gap-3">
                    <div className="grid size-10 place-items-center rounded-lg bg-primary/10 text-primary">
                      <UserRound aria-hidden="true" className="size-icon-md" />
                    </div>
                    <Badge>Authoritative</Badge>
                  </div>
                  <CardTitle>Identity owns the tutor</CardTitle>
                  <CardDescription>
                    The gateway reads this source after checking the access token.
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  {thread.data ? (
                    <dl className="grid min-w-0 gap-y-3 text-sm sm:grid-cols-[minmax(5rem,auto)_minmax(0,1fr)] sm:gap-x-4">
                      <dt className="text-muted-foreground">tutor_id</dt>
                      <dd className="break-all font-mono text-xs">{thread.data.tutor.tutor_id}</dd>
                      <dt className="text-muted-foreground">name</dt>
                      <dd className="min-w-0 wrap-anywhere">{thread.data.tutor.display_name}</dd>
                      <dt className="text-muted-foreground">email</dt>
                      <dd className="break-all">{thread.data.tutor.email}</dd>
                      <dt className="text-muted-foreground">timezone</dt>
                      <dd className="break-all font-mono text-xs">{thread.data.tutor.timezone}</dd>
                    </dl>
                  ) : (
                    <div
                      role="status"
                      aria-label="Reading tutor"
                      aria-busy="true"
                      className="grid gap-3"
                    >
                      <Skeleton className="h-4 w-3/4" />
                      <Skeleton className="h-4 w-full" />
                      <Skeleton className="h-4 w-2/3" />
                    </div>
                  )}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <div className="mb-1 flex flex-wrap items-center justify-between gap-3">
                    <div className="grid size-10 place-items-center rounded-lg bg-primary/10 text-primary">
                      <Radio aria-hidden="true" className="size-icon-md" />
                    </div>
                    <Badge variant={recorded ? 'success' : 'warning'}>
                      {recorded ? 'Recorded' : 'Following the event'}
                    </Badge>
                  </div>
                  <CardTitle>Notifications heard the event</CardTitle>
                  <CardDescription>
                    This copy is a rebuildable projection, never the owner record.
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  {thread.data?.projection_unavailable ? (
                    <div className="rounded-lg border border-warning/35 bg-warning-surface p-4 text-sm leading-relaxed text-warning-foreground">
                      This projection is unavailable on its own:{' '}
                      {thread.data.projection_unavailable}
                    </div>
                  ) : recorded ? (
                    <div className="flex items-start gap-3 rounded-lg border border-success/35 bg-success-surface p-4 text-success-foreground">
                      <Check aria-hidden="true" className="mt-0.5 size-icon-md shrink-0" />
                      <p className="min-w-0 text-sm leading-relaxed wrap-anywhere">
                        Recorded at{' '}
                        <span className="break-all font-mono text-xs">
                          {thread.data?.projection.recorded_at}
                        </span>
                        . The event reached its consumer.
                      </p>
                    </div>
                  ) : (
                    <div className="flex items-start gap-3 rounded-lg border border-border bg-muted p-4">
                      <BookOpen
                        aria-hidden="true"
                        className="mt-0.5 size-icon-md shrink-0 text-primary"
                      />
                      <p className="text-sm leading-relaxed text-muted-foreground">
                        Not recorded yet. The relay wakes on a timer, so this takes about a second.
                        That wait is the design working.
                      </p>
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>

            <Card>
              <CardHeader>
                <CardTitle>The path this page proves</CardTitle>
                <CardDescription>
                  No service calls another service. The business write and its outbox fact travel
                  through one event path.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <ol className="grid gap-3 sm:grid-cols-4">
                  {[
                    'Identity write',
                    'Transactional outbox',
                    'Redpanda topic',
                    'Notifications projection',
                  ].map((step, index, steps) => (
                    <li key={step} className="flex min-w-0 items-center gap-3">
                      <span className="grid size-8 shrink-0 place-items-center rounded-full bg-primary/10 font-mono text-xs font-semibold text-primary">
                        {index + 1}
                      </span>
                      <span className="min-w-0 text-sm font-medium wrap-anywhere">{step}</span>
                      {index < steps.length - 1 && (
                        <ArrowRight
                          aria-hidden="true"
                          className="ms-auto hidden size-icon-sm shrink-0 text-muted-foreground sm:block"
                        />
                      )}
                    </li>
                  ))}
                </ol>
              </CardContent>
            </Card>
          </section>
        )}
      </PageEntrance>
    </AppShell>
  )
}
