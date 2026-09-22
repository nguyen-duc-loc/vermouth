import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Landmark, LogOut } from 'lucide-react'
import { useState } from 'react'

import { cancelBillingClientRequests, clearBillingClientState } from '../api/billing'
import {
  cancelProfileClientRequests,
  clearProfileClientState,
  profileKeys,
  readInvoiceProfile,
} from '../api/profile'
import { signOut } from '../api/session'
import { readTutor } from '../api/teaching'
import { clearAllTeachingDrafts } from '../lib/teaching-draft'
import { AppearancePanel, type AppearancePanelText } from './AppearancePanel'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Separator } from './ui/separator'

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

export type AccountPanelProps = {
  beforeSignOut?: () => boolean | Promise<boolean>
}

/** Keeps private profile status, appearance, and sign out in one account surface. */
export function AccountPanel({ beforeSignOut }: AccountPanelProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [signingOut, setSigningOut] = useState(false)
  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const tutorId = tutorQuery.data?.tutor_id ?? ''
  const profileQuery = useQuery({
    queryKey: profileKeys.detail(tutorId),
    queryFn: ({ signal }) => readInvoiceProfile(signal),
    enabled: tutorId !== '' && !signingOut,
    staleTime: 0,
  })

  async function endSession() {
    if ((await beforeSignOut?.()) === false) return
    setSigningOut(true)
    clearAllTeachingDrafts()
    await Promise.all([
      cancelProfileClientRequests(queryClient),
      cancelBillingClientRequests(queryClient),
    ])
    const session = await signOut()
    await Promise.all([clearProfileClientState(queryClient), clearBillingClientState(queryClient)])
    if (session.status === 'anonymous') {
      await navigate({ to: '/signin', search: { redirect: '/' } })
      return
    }
    setSigningOut(false)
  }

  return (
    <div className="grid gap-6">
      <section className="grid gap-3" aria-labelledby="invoice-profile-link-title">
        <Link
          to="/profile"
          className="flex min-h-11 items-center gap-3 rounded-lg border border-border bg-surface px-3 py-2 outline-none transition-colors duration-base hover:bg-muted focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background motion-reduce:transition-none"
        >
          <Landmark aria-hidden="true" className="size-icon-md shrink-0 text-primary" />
          <span className="min-w-0 flex-1">
            <span id="invoice-profile-link-title" className="block font-medium">
              Profile and bank details
            </span>
            <span className="block text-sm text-muted-foreground">
              Details used on future invoices
            </span>
          </span>
          {profileQuery.data && !profileQuery.data.is_complete ? (
            <Badge variant="warning">Incomplete</Badge>
          ) : null}
        </Link>
        {profileQuery.isError ? (
          <div className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
            <span role="status">Profile status unavailable</span>
            <Button variant="quiet" onClick={() => void profileQuery.refetch()}>
              Retry
            </Button>
          </div>
        ) : null}
      </section>
      <Separator />
      <AppearancePanel text={appearanceText} />
      <Separator />
      <Button variant="secondary" loading={signingOut} onClick={() => void endSession()}>
        <LogOut aria-hidden="true" className="size-icon-sm" />
        {signingOut ? 'Signing out…' : 'Sign out'}
      </Button>
    </div>
  )
}
