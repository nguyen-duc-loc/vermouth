import type { QueryClient } from '@tanstack/react-query'

import { api, authHeaders } from './client'
import type { components } from './schema'
import { withProtectedRetry } from './session'

export type ApiError = components['schemas']['Error']
export type Bank = components['schemas']['Bank']
export type BankCatalog = components['schemas']['BankCatalog']
export type InvoiceProfile = components['schemas']['InvoiceProfile']
export type PutInvoiceProfileInput = components['schemas']['PutInvoiceProfileRequest']

export const profileKeys = {
  all: ['invoice-profile'] as const,
  detail: (tutorId: string) => [...profileKeys.all, tutorId] as const,
}

export const bankKeys = {
  all: ['banks'] as const,
  list: (tutorId: string) => [...bankKeys.all, tutorId] as const,
}

const activeProfileRequests = new Set<AbortController>()

export class ProfileApiError extends Error {
  readonly body: ApiError

  constructor(body: ApiError, fallback: string) {
    super(body.error.message || fallback)
    this.name = 'ProfileApiError'
    this.body = body
  }
}

function throwProfileError(error: unknown, fallback: string): never {
  if (error && typeof error === 'object' && 'error' in error) {
    throw new ProfileApiError(error as ApiError, fallback)
  }
  throw new Error(fallback)
}

export async function readInvoiceProfile(signal?: AbortSignal): Promise<InvoiceProfile> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/invoice-profile', { headers: authHeaders(), signal }),
  )
  if (error || !data) throwProfileError(error, 'the invoice profile could not be read')
  return data
}

export async function readBanks(signal?: AbortSignal): Promise<BankCatalog> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/banks', { headers: authHeaders(), signal }),
  )
  if (error || !data) throwProfileError(error, 'the bank list could not be read')
  return data
}

export async function putInvoiceProfile(input: PutInvoiceProfileInput): Promise<InvoiceProfile> {
  const controller = new AbortController()
  activeProfileRequests.add(controller)
  try {
    const { data, error } = await withProtectedRetry(() =>
      api.PUT('/api/invoice-profile', {
        headers: authHeaders(),
        body: input,
        signal: controller.signal,
      }),
    )
    if (error || !data) throwProfileError(error, 'the invoice profile could not be saved')
    return data
  } finally {
    activeProfileRequests.delete(controller)
  }
}

export async function cancelProfileClientRequests(queryClient: QueryClient): Promise<void> {
  for (const controller of activeProfileRequests) controller.abort()
  activeProfileRequests.clear()
  await Promise.all([
    queryClient.cancelQueries({ queryKey: profileKeys.all }),
    queryClient.cancelQueries({ queryKey: bankKeys.all }),
  ])
}

export async function clearProfileClientState(queryClient: QueryClient): Promise<void> {
  await cancelProfileClientRequests(queryClient)
  queryClient.removeQueries({ queryKey: profileKeys.all })
  queryClient.removeQueries({ queryKey: bankKeys.all })
}
