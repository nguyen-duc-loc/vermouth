import { useSearch } from '@tanstack/react-router'

import { runtimeConfig } from '../api/runtime'
import { browserLanguage, googleSignInUrl, type SignInErrorCode } from '../api/session'

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
}

const unavailable = {
  en: 'Google sign in is not configured for this local environment. Use task dev:token for the development thread.',
  vi: 'Đăng nhập Google chưa được cấu hình cho môi trường cục bộ này. Hãy dùng task dev:token để chạy luồng phát triển.',
}

function refusalSentence(code: string | undefined): string | null {
  if (!code || !(code in refusals)) return null
  return refusals[code as SignInErrorCode][browserLanguage()]
}

export function SignInPage() {
  const { error } = useSearch({ from: '/signin' })
  const sentence = refusalSentence(error)
  const googleEnabled = runtimeConfig().googleAuthEnabled
  const unavailableMessage = unavailable[browserLanguage()]

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center gap-8 px-4 py-10">
      <header className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">Vermouth</h1>
        <p className="text-sm text-neutral-600">
          Sign in with the Google account you teach from. There is no password to invent, and none
          is stored anywhere.
        </p>
      </header>

      {sentence && (
        <p
          role="alert"
          className="rounded border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800"
        >
          {sentence}
        </p>
      )}

      {googleEnabled ? (
        <a
          href={googleSignInUrl('/')}
          className="flex min-h-11 items-center justify-center gap-3 rounded border border-neutral-300 bg-white px-4 py-3 font-medium text-neutral-900 hover:bg-neutral-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-neutral-900"
        >
          <GoogleMark />
          Continue with Google
        </a>
      ) : (
        <button
          type="button"
          disabled
          aria-describedby="google-auth-unavailable"
          className="flex min-h-11 items-center justify-center gap-3 rounded border border-neutral-300 bg-neutral-100 px-4 py-3 font-medium text-neutral-600 disabled:cursor-not-allowed"
        >
          <GoogleMark />
          Continue with Google
        </button>
      )}

      {!googleEnabled && (
        <p
          id="google-auth-unavailable"
          role="status"
          className="rounded border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900"
        >
          {unavailableMessage}
        </p>
      )}

      <p className="text-xs text-neutral-500">
        Only invited addresses can create an account. Signing in keeps you signed in on this device
        until you sign out.
      </p>
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
