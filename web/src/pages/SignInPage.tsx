import { useSearch } from '@tanstack/react-router'
import { CalendarCheck, Check, ShieldCheck } from 'lucide-react'
import { useEffect, useState } from 'react'

import { runtimeConfig } from '../api/runtime'
import {
  browserLanguage,
  googleSignInUrl,
  type SignInErrorCode,
  sessionCoordinator,
  useSession,
} from '../api/session'
import { ClassColorCard } from '../components/ClassColorCard'
import { PageEntrance } from '../components/PageEntrance'
import { Alert, AlertDescription } from '../components/ui/alert'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader } from '../components/ui/card'

// The sign in screen. One button, because there is one way in: Google. There is
// no password field here and no column behind one (spec 0004).
//
// Feature 6 owns the design system, so what is here is only what this screen
// needs: a phone first column, a target big enough to hit standing up, and a
// focus ring that is visible without a mouse.

/** The sentence each refusal gets, in both languages the app has. */
const refusals: Record<SignInErrorCode, { vi: string; en: string }> = {
  not_allowed: {
    vi: 'Email này chưa được mời. Hãy nhờ chủ hệ thống thêm địa chỉ của bạn, rồi thử lại.',
    en: 'That email is not invited yet. Ask the operator to add your address, then try again.',
  },
  email_conflict: {
    vi: 'Email này đã thuộc về một tài khoản khác. Hãy đăng nhập bằng tài khoản Google đã dùng lần đầu.',
    en: 'That email already belongs to another account. Sign in with the Google account you used first.',
  },
  cancelled: {
    vi: 'Bạn đã dừng ở bước Google. Không có gì được lưu lại.',
    en: 'You stopped at the Google step. Nothing was saved.',
  },
  expired_state: {
    vi: 'Lần đăng nhập đó đã quá cũ. Hãy bắt đầu lại từ đây.',
    en: 'That sign in took too long. Start again from here.',
  },
  provider_error: {
    vi: 'Google không trả lời được lúc này. Hãy thử lại sau một phút.',
    en: 'Google could not answer just now. Try again in a minute.',
  },
  rate_limited: {
    vi: 'Có quá nhiều yêu cầu đăng nhập. Hãy chờ một lát rồi thử lại.',
    en: 'Too many sign in requests. Wait a moment, then try again.',
  },
}

const unavailable = {
  en: 'Google sign in is not configured for this local environment. Use task dev:token for the development thread.',
  vi: 'Đăng nhập Google chưa được cấu hình cho môi trường cục bộ này. Hãy dùng task dev:token để chạy luồng phát triển.',
}

const retryCopy = {
  en: {
    ready: 'Try again',
    waiting: (seconds: number) => `Try again in ${seconds}s`,
  },
  vi: {
    ready: 'Thử lại',
    waiting: (seconds: number) => `Thử lại sau ${seconds} giây`,
  },
}

function refusalSentence(code: unknown): string | null {
  if (typeof code !== 'string' || !(code in refusals)) return null
  return refusals[code as SignInErrorCode][browserLanguage()]
}

export function SignInPage() {
  const { error, redirect } = useSearch({ from: '/signin' })
  const session = useSession()
  const language = browserLanguage()
  const [retryAt, setRetryAt] = useState(session.rateLimitedUntil ?? 0)
  const [now, setNow] = useState(Date.now())
  const [retrying, setRetrying] = useState(false)
  const bootRefreshLimited = session.rateLimitedUntil !== undefined
  const sentence = refusalSentence(bootRefreshLimited ? 'rate_limited' : error)
  const googleEnabled = runtimeConfig().googleAuthEnabled
  const unavailableMessage = unavailable[language]
  const secondsRemaining = Math.max(0, Math.ceil((retryAt - now) / 1_000))
  const retryLabel =
    secondsRemaining > 0 ? retryCopy[language].waiting(secondsRemaining) : retryCopy[language].ready

  useEffect(() => {
    document.title = 'Sign in · Vermouth'
  }, [])

  useEffect(() => {
    if (session.rateLimitedUntil === undefined) return
    setRetryAt(session.rateLimitedUntil)
    setNow(Date.now())
  }, [session.rateLimitedUntil])

  useEffect(() => {
    if (retryAt <= Date.now()) return
    const timer = window.setInterval(() => {
      const current = Date.now()
      setNow(current)
      if (current >= retryAt) window.clearInterval(timer)
    }, 250)
    return () => window.clearInterval(timer)
  }, [retryAt])

  async function retryRefresh() {
    if (Date.now() < retryAt) return
    setRetrying(true)
    try {
      await sessionCoordinator.retry()
    } finally {
      setRetrying(false)
    }
  }

  return (
    <main className="min-h-screen bg-background text-foreground">
      <PageEntrance className="grid min-h-screen lg:grid-cols-[minmax(0,1.05fr)_minmax(28rem,0.95fr)]">
        <section
          data-entrance-item
          aria-labelledby="welcome-title"
          className="relative hidden overflow-hidden border-e border-border bg-muted px-8 py-10 lg:flex lg:flex-col lg:justify-between xl:px-14"
        >
          <a
            href="/"
            className="flex w-fit items-center gap-3 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          >
            <span className="grid size-11 place-items-center rounded-xl bg-primary text-lg font-semibold text-primary-foreground">
              V
            </span>
            <span className="text-lg font-semibold">Vermouth</span>
          </a>

          <div className="grid max-w-xl gap-8 py-12">
            <div className="grid gap-3">
              <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
                The teaching day, in one place
              </p>
              <h1
                id="welcome-title"
                className="text-3xl font-semibold text-balance xl:text-[2.5rem] xl:leading-tight"
              >
                Keep the class in front of you, not the paperwork.
              </h1>
              <p className="max-w-lg text-base leading-relaxed text-muted-foreground">
                Plan sessions, mark attendance, and turn the month into clear invoices without
                losing the rhythm of teaching.
              </p>
            </div>

            <div className="grid gap-3 rounded-xl border border-border bg-surface p-3 shadow-raised sm:grid-cols-2">
              <div className="grid gap-2 rounded-lg bg-muted p-3">
                <span className="font-mono text-xs text-muted-foreground">07:30</span>
                <ClassColorCard color="blue" title="Mathematics · 8A" detail="12 students" />
              </div>
              <div className="grid gap-2 rounded-lg bg-muted p-3">
                <span className="font-mono text-xs text-muted-foreground">09:15</span>
                <ClassColorCard color="green" title="Physics · 10" detail="8 students" />
              </div>
            </div>

            <ul className="grid gap-3 text-sm text-muted-foreground sm:grid-cols-2">
              <li className="flex items-start gap-2">
                <Check aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0 text-success" />
                Attendance that works on a phone
              </li>
              <li className="flex items-start gap-2">
                <Check aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0 text-success" />
                VND invoices without spreadsheet drift
              </li>
            </ul>
          </div>

          <p className="text-xs text-muted-foreground">Private workspace for invited tutors.</p>
        </section>

        <section
          data-entrance-item
          className="flex min-h-screen items-center px-4 py-8 sm:px-8 lg:px-12 xl:px-20"
        >
          <div className="mx-auto grid w-full max-w-md gap-6">
            <header className="grid gap-4 lg:hidden">
              <div className="flex flex-wrap items-center gap-3">
                <span className="grid size-11 place-items-center rounded-xl bg-primary text-lg font-semibold text-primary-foreground">
                  V
                </span>
                <span className="min-w-0 text-lg font-semibold wrap-anywhere">Vermouth</span>
              </div>
              <div className="grid gap-2">
                <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
                  Tutor workspace
                </p>
                <h1 className="text-2xl font-semibold text-balance">
                  Come back to your teaching day.
                </h1>
              </div>
            </header>

            <Card className="shadow-raised">
              <CardHeader>
                <div className="mb-2 grid size-12 place-items-center rounded-xl bg-primary/10 text-primary">
                  <CalendarCheck aria-hidden="true" className="size-icon-lg" />
                </div>
                <h2 className="text-lg font-semibold">Sign in to Vermouth</h2>
                <CardDescription>
                  Use the Google account you teach from. There is no password to invent, and none is
                  stored anywhere.
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-4">
                {sentence && (
                  <Alert role="alert" variant="destructive">
                    <AlertDescription>{sentence}</AlertDescription>
                  </Alert>
                )}

                {bootRefreshLimited && (
                  <div className="grid gap-2">
                    <Button
                      type="button"
                      size="large"
                      className="w-full"
                      disabled={secondsRemaining > 0}
                      loading={retrying}
                      aria-describedby="refresh-retry-status"
                      onClick={retryRefresh}
                    >
                      {retryLabel}
                    </Button>
                    <p
                      id="refresh-retry-status"
                      role="status"
                      aria-live="polite"
                      aria-atomic="true"
                      className="sr-only"
                    >
                      {retryLabel}
                    </p>
                  </div>
                )}

                {googleEnabled ? (
                  <Button asChild variant="secondary" size="large" className="w-full">
                    <a href={googleSignInUrl(redirect)}>
                      <GoogleMark />
                      Continue with Google
                    </a>
                  </Button>
                ) : (
                  <Button
                    disabled
                    size="large"
                    variant="secondary"
                    aria-describedby="google-auth-unavailable"
                    className="w-full"
                  >
                    <GoogleMark />
                    Continue with Google
                  </Button>
                )}

                {!googleEnabled && (
                  <Alert id="google-auth-unavailable" role="status" variant="warning">
                    <AlertDescription>{unavailableMessage}</AlertDescription>
                  </Alert>
                )}

                <div className="flex items-start gap-2 border-t border-border pt-4 text-xs leading-relaxed text-muted-foreground">
                  <ShieldCheck aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
                  <p>
                    Only invited addresses can create an account. Signing in keeps you signed in on
                    this device until you sign out.
                  </p>
                </div>
              </CardContent>
            </Card>
          </div>
        </section>
      </PageEntrance>
    </main>
  )
}

// The Google mark, inline so the screen needs no network round trip to draw its
// one button. It is decorative: the button's own text says what it does.
function GoogleMark() {
  return (
    <svg aria-hidden="true" className="size-5" viewBox="0 0 48 48">
      <path
        fill="#EA4335"
        d="M24 9.5c3.5 0 6.6 1.2 9 3.6l6.8-6.8C35.6 2.5 30.1 0 24 0 14.6 0 6.5 5.4 2.6 13.2l7.9 6.2C12.4 13.3 17.7 9.5 24 9.5z"
      />
      <path
        fill="#4285F4"
        d="M46.1 24.5c0-1.6-.1-2.8-.4-4.1H24v8.4h12.6c-.3 2.1-1.6 5.2-4.6 7.3l7.7 6c4.5-4.2 6.4-10.1 6.4-17.6z"
      />
      <path
        fill="#FBBC05"
        d="M10.5 28.6A14.6 14.6 0 0 1 9.7 24c0-1.6.3-3.2.8-4.6l-7.9-6.2A24 24 0 0 0 0 24c0 3.9.9 7.5 2.6 10.8l7.9-6.2z"
      />
      <path
        fill="#34A853"
        d="M24 48c6.5 0 11.9-2.1 15.7-5.8l-7.7-6c-2.1 1.4-4.8 2.3-8 2.3-6.3 0-11.6-3.8-13.5-9.1l-7.9 6.2C6.5 42.6 14.6 48 24 48z"
      />
    </svg>
  )
}
