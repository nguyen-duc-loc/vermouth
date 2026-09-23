import type { QueryClient } from '@tanstack/react-query'

import { api, authHeaders } from './client'
import type { components } from './schema'
import { withProtectedRetry } from './session'

export type ApiError = components['schemas']['Error']
export type BillingPeriodDefault = components['schemas']['BillingPeriodDefault']
export type BillingPeriodState = components['schemas']['BillingPeriodState']
export type BillingPreview = components['schemas']['BillingPreview']
export type BillingRun = components['schemas']['BillingRun']
export type ClassRates = components['schemas']['ClassRates']
export type PutClassRateInput = components['schemas']['PutClassRateRequest']
export type PutClassRateResult = components['schemas']['PutClassRateResponse']

export const billingKeys = {
  all: ['billing'] as const,
  tutor: (tutorId: string) => [...billingKeys.all, tutorId] as const,
  defaultPeriod: (tutorId: string) => [...billingKeys.tutor(tutorId), 'default'] as const,
  periods: (tutorId: string) => [...billingKeys.tutor(tutorId), 'periods'] as const,
  period: (tutorId: string, year: number, month: number) =>
    [...billingKeys.periods(tutorId), year, month] as const,
  preview: (tutorId: string, year: number, month: number) =>
    [...billingKeys.period(tutorId, year, month), 'preview'] as const,
}

export const classRateKeys = {
  all: ['class-rates'] as const,
  detail: (tutorId: string, classId: string) => [...classRateKeys.all, tutorId, classId] as const,
}

const activeBillingRequests = new Set<AbortController>()

export class BillingApiError extends Error {
  readonly body: ApiError

  constructor(body: ApiError, fallback: string) {
    super(body.error.message || fallback)
    this.name = 'BillingApiError'
    this.body = body
  }
}

function throwBillingError(error: unknown, fallback: string): never {
  if (error && typeof error === 'object' && 'error' in error) {
    throw new BillingApiError(error as ApiError, fallback)
  }
  throw new Error(fallback)
}

export async function readClassRates(classId: string, signal?: AbortSignal): Promise<ClassRates> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/classes/{class_id}/rates', {
      headers: authHeaders(),
      params: { path: { class_id: classId } },
      signal,
    }),
  )
  if (error || !data) throwBillingError(error, 'the class rate history could not be read')
  return data
}

export async function putClassRate(
  classId: string,
  effectiveDate: string,
  input: PutClassRateInput,
  idempotencyKey: string,
): Promise<PutClassRateResult> {
  const controller = new AbortController()
  activeBillingRequests.add(controller)
  try {
    const { data, error } = await withProtectedRetry(() =>
      api.PUT('/api/classes/{class_id}/rates/{effective_date}', {
        headers: authHeaders(),
        params: {
          path: { class_id: classId, effective_date: effectiveDate },
          header: { 'Idempotency-Key': idempotencyKey },
        },
        body: input,
        signal: controller.signal,
      }),
    )
    if (error || !data) throwBillingError(error, 'the class rate could not be saved')
    return data
  } finally {
    activeBillingRequests.delete(controller)
  }
}

export async function readBillingPeriodDefault(
  signal?: AbortSignal,
): Promise<BillingPeriodDefault> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/billing-periods/default', { headers: authHeaders(), signal }),
  )
  if (error || !data) throwBillingError(error, 'the default billing month could not be read')
  return data
}

export async function readBillingPeriod(
  year: number,
  month: number,
  signal?: AbortSignal,
): Promise<BillingPeriodState> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/billing-periods/{year}/{month}', {
      headers: authHeaders(),
      params: { path: { year, month } },
      signal,
    }),
  )
  if (error || !data) throwBillingError(error, 'the billing month could not be read')
  return data
}

export async function previewBillingPeriod(
  year: number,
  month: number,
  signal?: AbortSignal,
): Promise<BillingPreview> {
  const { data, error } = await withProtectedRetry(() =>
    api.POST('/api/billing-periods/{year}/{month}/preview', {
      headers: authHeaders(),
      params: { path: { year, month } },
      signal,
    }),
  )
  if (error || !data) throwBillingError(error, 'the billing preview could not be calculated')
  return data
}

export async function issueBillingPeriod(
  year: number,
  month: number,
  previewFingerprint: string,
): Promise<BillingRun> {
  const controller = new AbortController()
  activeBillingRequests.add(controller)
  try {
    const { data, error } = await withProtectedRetry(() =>
      api.POST('/api/billing-periods/{year}/{month}/issue', {
        headers: authHeaders(),
        params: { path: { year, month } },
        body: { preview_fingerprint: previewFingerprint },
        signal: controller.signal,
      }),
    )
    if (error || !data) throwBillingError(error, 'the billing month could not be issued')
    return data
  } finally {
    activeBillingRequests.delete(controller)
  }
}

export async function cancelBillingClientRequests(queryClient: QueryClient): Promise<void> {
  for (const controller of activeBillingRequests) controller.abort()
  activeBillingRequests.clear()
  await Promise.all([
    queryClient.cancelQueries({ queryKey: billingKeys.all }),
    queryClient.cancelQueries({ queryKey: classRateKeys.all }),
  ])
}

export async function clearBillingClientState(queryClient: QueryClient): Promise<void> {
  await cancelBillingClientRequests(queryClient)
  queryClient.removeQueries({ queryKey: billingKeys.all })
  queryClient.removeQueries({ queryKey: classRateKeys.all })
}
