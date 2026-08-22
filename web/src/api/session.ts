import { api, setAccessToken } from './client'
import type { components } from './schema'

/** What a refresh answers with. The refresh token itself is never in a body. */
export type Session = components['schemas']['Session']

/** The five reasons the sign in screen has a sentence for (spec 0004). */
export type SignInErrorCode =
  | 'not_allowed'
  | 'email_conflict'
  | 'cancelled'
  | 'expired_state'
  | 'provider_error'

/**
 * Where the Google button goes. It is a plain link rather than a fetch, because
 * the browser itself has to follow the redirect to Google. The timezone and the
 * language travel with it, since the callback is what creates the tutor and by
 * then this browser is no longer in the conversation.
 */
export function googleSignInUrl(redirectTo: string): string {
  const params = new URLSearchParams({
    tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
    lang: browserLanguage(),
    redirect_to: redirectTo,
  })
  return `/api/auth/google/start?${params.toString()}`
}

/** vi or en, the two languages the app has. Anything else is treated as vi. */
export function browserLanguage(): 'vi' | 'en' {
  return navigator.language.toLowerCase().startsWith('en') ? 'en' : 'vi'
}

/**
 * The one path to an access token, called on boot and after the callback alike.
 * A refusal is not an error here: it is the answer to "is anybody signed in",
 * and it is the only place that question is ever asked, since no flag is stored.
 */
export async function refreshSession(): Promise<Session | null> {
  const { data, error } = await api.POST('/api/auth/refresh', {})
  if (error || !data) {
    setAccessToken(null)
    return null
  }
  setAccessToken(data.access_token)
  return data
}

/** Ends the session at identity, then forgets the token this tab held. */
export async function signOut(): Promise<void> {
  await api.POST('/api/auth/signout', {})
  setAccessToken(null)
}
