import { api, authHeaders } from './client'
import type { components } from './schema'
import { withProtectedRetry } from './session'

export type Tutor = components['schemas']['Tutor']
export type ThreadStatus = components['schemas']['ThreadStatus']
/** The one error shape at the gateway boundary (spec 0001). */
export type ApiError = components['schemas']['Error']

function message(error: unknown, fallback: string) {
  const shaped = error as ApiError | undefined
  return shaped?.error?.message ?? fallback
}

export async function readThread(): Promise<ThreadStatus> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/thread', { headers: authHeaders() }),
  )
  if (error || !data) throw new Error(message(error, 'the gateway could not read the thread'))
  return data
}
