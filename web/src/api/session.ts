import { useSyncExternalStore } from 'react'

import { api, setAccessToken } from './client'
import type { components } from './schema'

/** What a refresh answers with. The refresh token itself is never in a body. */
export type Session = components['schemas']['Session']

/** The fixed refusal reasons the sign in screen is allowed to display. */
export type SignInErrorCode =
  | 'not_allowed'
  | 'email_conflict'
  | 'cancelled'
  | 'expired_state'
  | 'provider_error'
  | 'rate_limited'

/** The three typed outcomes of exchanging the refresh cookie. */
export type RefreshOutcome =
  | { status: 'signed_in'; session: Session }
  | { status: 'signed_out' }
  | { status: 'rate_limited'; retry_at: number }

/** The browser states that decide whether protected content may render. */
export type SessionStatus = 'checking' | 'authenticated' | 'anonymous' | 'unavailable'

/** A stable snapshot for route guards and accessible state surfaces. */
export type SessionSnapshot = {
  status: SessionStatus
  message?: string
  rateLimitedUntil?: number
}

type ProtectedResult<T> = {
  data?: T
  error?: unknown
  response: Response
}

const renewalLeadMs = 60_000
const firstRetryMs = 1_000
const maxRetryMs = 5_000

const unavailableMessage =
  'Vermouth could not confirm this session. Check the connection, then try again.'

const knownSignInErrors = new Set<SignInErrorCode>([
  'not_allowed',
  'email_conflict',
  'cancelled',
  'expired_state',
  'provider_error',
  'rate_limited',
])

function isSignInErrorCode(value: unknown): value is SignInErrorCode {
  return typeof value === 'string' && knownSignInErrors.has(value as SignInErrorCode)
}

function containsControlCharacter(value: string): boolean {
  for (const character of value) {
    const code = character.codePointAt(0)
    if (code !== undefined && (code <= 31 || code === 127)) return true
  }
  return false
}

/** Keeps only the five callback refusal codes the sign in page understands. */
export function cleanSignInError(value: unknown): SignInErrorCode | undefined {
  return isSignInErrorCode(value) ? value : undefined
}

/**
 * Keeps one clean relative app path and query. Any value that could choose an
 * origin, hide a second leading slash, or carry a fragment falls back to root.
 */
export function cleanRedirect(raw: unknown): string {
  if (typeof raw !== 'string') return '/'
  const target = raw.trim()
  if (
    target === '' ||
    !target.startsWith('/') ||
    target.startsWith('//') ||
    target.includes('\\') ||
    target.includes('#') ||
    containsControlCharacter(target)
  ) {
    return '/'
  }
  try {
    const parsed = new URL(target, 'https://vermouth.invalid')
    const decodedPath = decodeURIComponent(parsed.pathname)
    const decodedSearch = decodeURIComponent(parsed.search)
    if (
      parsed.origin !== 'https://vermouth.invalid' ||
      decodedPath.startsWith('//') ||
      decodedPath.includes('\\') ||
      containsControlCharacter(decodedPath) ||
      decodedSearch.includes('\\') ||
      containsControlCharacter(decodedSearch)
    ) {
      return '/'
    }
    return `${parsed.pathname}${parsed.search}`
  } catch {
    return '/'
  }
}

/**
 * Where the Google button goes. It is a plain link because the browser itself
 * follows the provider redirect. Creation defaults travel with the attempt.
 */
export function googleSignInUrl(redirectTo: string): string {
  const params = new URLSearchParams({
    tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
    lang: browserLanguage(),
    redirect_to: cleanRedirect(redirectTo),
  })
  return `/api/auth/google/start?${params.toString()}`
}

/** Uses the first supported browser language, then falls back to Vietnamese. */
export function browserLanguage(): 'vi' | 'en' {
  for (const language of navigator.languages) {
    const normalized = language.toLowerCase()
    if (normalized.startsWith('vi')) return 'vi'
    if (normalized.startsWith('en')) return 'en'
  }
  return 'vi'
}

class SessionCoordinator {
  private snapshot: SessionSnapshot = { status: 'checking' }
  private readonly listeners = new Set<() => void>()
  private refreshPromise: Promise<RefreshOutcome> | null = null
  private accessExpiresAt = 0
  private renewalTimer: number | undefined
  private retryTimer: number | undefined
  private retryDelay = firstRetryMs
  private visibilityStarted = false
  private retryAction: (() => Promise<void>) | undefined
  private memoryGeneration = 0
  private rateLimitedUntil: number | undefined

  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  readonly getSnapshot = (): SessionSnapshot => this.snapshot

  start(): Promise<RefreshOutcome> {
    if (!this.visibilityStarted) {
      document.addEventListener('visibilitychange', this.onVisibilityChange)
      this.visibilityStarted = true
    }
    return this.refresh()
  }

  async ensure(): Promise<SessionSnapshot> {
    if (this.snapshot.status === 'checking') {
      await (this.refreshPromise ?? this.start())
    }
    return this.snapshot
  }

  refresh(): Promise<RefreshOutcome> {
    if (this.refreshPromise) return this.refreshPromise
    if (this.rateLimitedUntil !== undefined && Date.now() < this.rateLimitedUntil) {
      return Promise.resolve({ status: 'rate_limited', retry_at: this.rateLimitedUntil })
    }
    const promise = this.performRefresh().finally(() => {
      if (this.refreshPromise === promise) this.refreshPromise = null
    })
    this.refreshPromise = promise
    return promise
  }

  async signOut(): Promise<SessionSnapshot> {
    this.clearMemory()
    this.update({ status: 'checking' })
    try {
      const result = await api.POST('/api/auth/signout', {})
      if (result.response.status === 204) {
        this.retryAction = undefined
        this.update({ status: 'anonymous' })
        return this.snapshot
      }
    } catch {
      // The unavailable state below owns the recovery action.
    }
    this.retryAction = () => this.signOut().then(() => undefined)
    this.update({ status: 'unavailable', message: unavailableMessage })
    return this.snapshot
  }

  async retry(): Promise<void> {
    if (this.retryAction) {
      await this.retryAction()
      return
    }
    await this.refresh()
  }

  refuse(): void {
    this.clearMemory()
    this.retryAction = undefined
    this.update({ status: 'anonymous' })
  }

  private async performRefresh(): Promise<RefreshOutcome> {
    const memoryGeneration = this.memoryGeneration
    const hadValidAccess = this.hasValidAccess()
    this.update({ status: 'checking', rateLimitedUntil: this.rateLimitedUntil })
    try {
      const result = await api.POST('/api/auth/refresh', {})
      if (memoryGeneration !== this.memoryGeneration) return { status: 'signed_out' }
      if (result.data) {
        this.accept(result.data)
        return { status: 'signed_in', session: result.data }
      }
      if (result.response.status === 401) {
        this.refuse()
        return { status: 'signed_out' }
      }
      if (result.response.status === 429) {
        const retryAt = retryInstant(result.response)
        if (retryAt !== undefined) {
          this.handleRateLimit(hadValidAccess, retryAt)
          return { status: 'rate_limited', retry_at: retryAt }
        }
      }
    } catch {
      // The transient failure path below decides whether the old token survives.
    }
    if (memoryGeneration !== this.memoryGeneration) return { status: 'signed_out' }
    this.handleRefreshFailure(hadValidAccess)
    return { status: 'signed_out' }
  }

  private accept(session: Session): void {
    const expiresAt = Date.parse(session.access_expires_at)
    if (!Number.isFinite(expiresAt)) {
      this.clearMemory()
      this.retryAction = () => this.refresh().then(() => undefined)
      this.update({ status: 'unavailable', message: unavailableMessage })
      return
    }
    setAccessToken(session.access_token)
    this.accessExpiresAt = expiresAt
    this.retryDelay = firstRetryMs
    this.retryAction = undefined
    this.rateLimitedUntil = undefined
    this.clearRetryTimer()
    this.scheduleRenewal()
    this.update({ status: 'authenticated' })
  }

  private handleRefreshFailure(hadValidAccess: boolean): void {
    if (hadValidAccess && this.hasValidAccess()) {
      this.update({ status: 'authenticated' })
      this.clearRetryTimer()
      this.retryTimer = window.setTimeout(() => void this.refresh(), this.retryDelay)
      this.retryDelay = Math.min(this.retryDelay * 2, maxRetryMs)
      return
    }
    this.clearMemory()
    this.retryAction = () => this.refresh().then(() => undefined)
    this.update({ status: 'unavailable', message: unavailableMessage })
  }

  private handleRateLimit(hadValidAccess: boolean, retryAt: number): void {
    this.rateLimitedUntil = retryAt
    this.retryAction = undefined
    if (this.renewalTimer !== undefined) window.clearTimeout(this.renewalTimer)
    this.renewalTimer = undefined
    this.clearRetryTimer()
    this.update({
      status: hadValidAccess && this.hasValidAccess() ? 'authenticated' : 'anonymous',
      rateLimitedUntil: retryAt,
    })
  }

  private scheduleRenewal(): void {
    if (this.renewalTimer !== undefined) window.clearTimeout(this.renewalTimer)
    const delay = Math.max(0, this.accessExpiresAt - renewalLeadMs - Date.now())
    this.renewalTimer = window.setTimeout(() => void this.refresh(), Math.min(delay, 2_147_483_647))
  }

  private readonly onVisibilityChange = (): void => {
    if (
      document.visibilityState === 'visible' &&
      this.accessExpiresAt - Date.now() <= renewalLeadMs
    ) {
      void this.refresh()
    }
  }

  private hasValidAccess(): boolean {
    return this.accessExpiresAt > Date.now()
  }

  private clearMemory(): void {
    this.memoryGeneration += 1
    setAccessToken(null)
    this.accessExpiresAt = 0
    this.rateLimitedUntil = undefined
    if (this.renewalTimer !== undefined) window.clearTimeout(this.renewalTimer)
    this.renewalTimer = undefined
    this.clearRetryTimer()
  }

  private clearRetryTimer(): void {
    if (this.retryTimer !== undefined) window.clearTimeout(this.retryTimer)
    this.retryTimer = undefined
  }

  private update(snapshot: SessionSnapshot): void {
    this.snapshot = snapshot
    for (const listener of this.listeners) listener()
  }
}

/** The one session authority shared by routes, requests, and visible states. */
export const sessionCoordinator = new SessionCoordinator()

function retryInstant(response: Response): number | undefined {
  const raw = response.headers.get('Retry-After')
  if (raw === null || !/^[1-9][0-9]*$/.test(raw)) return undefined
  const seconds = Number(raw)
  if (!Number.isSafeInteger(seconds)) return undefined
  return Date.now() + seconds * 1_000
}

/** Subscribes a component to the current session state. */
export function useSession(): SessionSnapshot {
  return useSyncExternalStore(
    sessionCoordinator.subscribe,
    sessionCoordinator.getSnapshot,
    sessionCoordinator.getSnapshot,
  )
}

/** The one path to an access token, retained as the public boot helper. */
export function refreshSession(): Promise<RefreshOutcome> {
  return sessionCoordinator.start()
}

/** Clears memory first, then asks identity to revoke the locked family. */
export function signOut(): Promise<SessionSnapshot> {
  return sessionCoordinator.signOut()
}

/** Refreshes once and retries one protected request after its first 401. */
export async function withProtectedRetry<T>(
  request: () => Promise<ProtectedResult<T>>,
): Promise<ProtectedResult<T>> {
  const first = await request()
  if (first.response.status !== 401) return first
  const refreshed = await sessionCoordinator.refresh()
  if (refreshed.status !== 'signed_in') return first
  const second = await request()
  if (second.response.status === 401) sessionCoordinator.refuse()
  return second
}
