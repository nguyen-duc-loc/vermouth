import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'

import { signOut } from '../api/session'
import { readThread } from '../api/thread'

// The skeleton's one screen. It exists to show the whole pipe working from a
// browser: a signed in read through the gateway, and a projection in
// notifications that caught up a moment after the write.
//
// The tutor arrives here already signed in: the route redirects to /signin
// otherwise, and the token came from the boot time refresh (spec 0004).
//
// Feature 6 owns the design system, and feature 8 replaces this screen with the
// real teaching loop. Nothing here is meant to survive that.
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

  return (
    <main className="mx-auto flex max-w-xl flex-col gap-8 px-4 py-10">
      <header className="flex flex-col gap-3">
        <h1 className="text-2xl font-semibold">Vermouth skeleton</h1>
        <p className="text-sm text-neutral-600">
          One thread, end to end: the gateway, identity, its outbox, the relay, Redpanda, and
          notifications recording what it heard.
        </p>
        <button
          type="button"
          disabled={signingOut}
          onClick={async () => {
            setSigningOut(true)
            await signOut()
            await navigate({ to: '/signin' })
          }}
          className="min-h-11 self-start rounded border border-neutral-300 bg-white px-4 py-2 text-sm font-medium disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-neutral-900"
        >
          {signingOut ? 'Signing out…' : 'Sign out'}
        </button>
      </header>

      <section aria-labelledby="thread-heading" className="flex flex-col gap-3">
        <h2 id="thread-heading" className="text-lg font-medium">
          Watch a fact cross the broker
        </h2>
        <div aria-live="polite" className="flex flex-col gap-3">
          <Panel title="identity, which owns the tutor">
            {thread.data ? (
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                <dt className="text-neutral-500">tutor_id</dt>
                <dd className="font-mono text-xs">{thread.data.tutor.tutor_id}</dd>
                <dt className="text-neutral-500">name</dt>
                <dd>{thread.data.tutor.display_name}</dd>
                <dt className="text-neutral-500">email</dt>
                <dd>{thread.data.tutor.email}</dd>
                <dt className="text-neutral-500">timezone</dt>
                <dd>{thread.data.tutor.timezone}</dd>
              </dl>
            ) : (
              <p className="text-sm text-neutral-600">Reading…</p>
            )}
          </Panel>

          <Panel title="notifications, which heard about it through the broker">
            {thread.data?.projection_unavailable ? (
              <p className="text-sm text-amber-700">
                This panel is unavailable on its own: {thread.data.projection_unavailable}
              </p>
            ) : recorded ? (
              <p className="text-sm text-green-800">
                Recorded at {thread.data?.projection.recorded_at}. The event reached its consumer.
              </p>
            ) : (
              <p className="text-sm text-neutral-600">
                Not recorded yet. The relay wakes on a timer, so this takes about a second. That
                wait is the design working.
              </p>
            )}
          </Panel>
        </div>
        {thread.error && (
          <p role="alert" className="text-sm text-red-700">
            {thread.error.message}
          </p>
        )}
      </section>
    </main>
  )
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <article className="rounded border border-neutral-200 bg-white p-4">
      <h3 className="mb-2 text-sm font-semibold text-neutral-700">{title}</h3>
      {children}
    </article>
  )
}
