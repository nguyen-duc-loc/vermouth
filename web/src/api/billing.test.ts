import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  authHeaders: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  withProtectedRetry: vi.fn(),
}))

vi.mock('./client', () => ({
  api: { GET: client.get, POST: client.post, PUT: client.put },
  authHeaders: client.authHeaders,
}))

vi.mock('./session', () => ({ withProtectedRetry: client.withProtectedRetry }))

beforeEach(() => {
  client.authHeaders.mockReturnValue({ Authorization: 'Bearer access-token' })
  client.withProtectedRetry.mockImplementation((request: () => Promise<unknown>) => request())
  client.get.mockReset()
  client.post.mockReset()
  client.put.mockReset()
})

describe('billing API', () => {
  // covers: AC-1, AC-2, AC-4, AC-21
  it('uses generated paths and tutor scoped query keys', async () => {
    const classRates = { rates: [], projected_revision: 0 }
    const defaultPeriod = {
      server_date: '2026-09-22',
      timezone: 'Asia/Ho_Chi_Minh',
      year: 2026,
      month: 8,
      minimum_year: 2000,
    }
    client.get
      .mockResolvedValueOnce({ data: classRates })
      .mockResolvedValueOnce({ data: defaultPeriod })
    const { billingKeys, classRateKeys, readBillingPeriodDefault, readClassRates } = await import(
      './billing'
    )
    const rateSignal = new AbortController().signal
    const periodSignal = new AbortController().signal

    await expect(readClassRates('class-1', rateSignal)).resolves.toBe(classRates)
    await expect(readBillingPeriodDefault(periodSignal)).resolves.toBe(defaultPeriod)

    expect(classRateKeys.detail('tutor-1', 'class-1')).toEqual([
      'class-rates',
      'tutor-1',
      'class-1',
    ])
    expect(billingKeys.preview('tutor-1', 2026, 8)).toEqual([
      'billing',
      'tutor-1',
      'periods',
      2026,
      8,
      'preview',
    ])
    expect(client.get).toHaveBeenNthCalledWith(1, '/api/classes/{class_id}/rates', {
      headers: { Authorization: 'Bearer access-token' },
      params: { path: { class_id: 'class-1' } },
      signal: rateSignal,
    })
    expect(client.get).toHaveBeenNthCalledWith(2, '/api/billing-periods/default', {
      headers: { Authorization: 'Bearer access-token' },
      signal: periodSignal,
    })
  })

  // covers: AC-2, AC-13, AC-17, AC-21
  it('sends receipt and preview fingerprints and preserves structured errors', async () => {
    const rateResult = { rate_revision: 2 }
    const issuedRun = { billing_run_id: 'run-1' }
    client.put.mockResolvedValueOnce({ data: rateResult })
    client.post.mockResolvedValueOnce({ data: issuedRun })
    const { issueBillingPeriod, putClassRate } = await import('./billing')

    await expect(
      putClassRate('class-1', '2026-09-01', { rate_amount: 300_000 }, 'rate-command'),
    ).resolves.toBe(rateResult)
    await expect(issueBillingPeriod(2026, 8, 'preview-digest')).resolves.toBe(issuedRun)

    expect(client.put).toHaveBeenCalledWith('/api/classes/{class_id}/rates/{effective_date}', {
      headers: { Authorization: 'Bearer access-token' },
      params: {
        path: { class_id: 'class-1', effective_date: '2026-09-01' },
        header: { 'Idempotency-Key': 'rate-command' },
      },
      body: { rate_amount: 300_000 },
      signal: expect.any(AbortSignal),
    })
    expect(client.post).toHaveBeenCalledWith('/api/billing-periods/{year}/{month}/issue', {
      headers: { Authorization: 'Bearer access-token' },
      params: { path: { year: 2026, month: 8 } },
      body: { preview_fingerprint: 'preview-digest' },
      signal: expect.any(AbortSignal),
    })

    const stale = {
      error: {
        code: 'preview_stale',
        message: 'the billing inputs changed after preview',
        request_id: 'request-billing',
      },
    }
    client.post.mockResolvedValueOnce({ error: stale })
    await expect(issueBillingPeriod(2026, 8, 'stale-digest')).rejects.toMatchObject({
      name: 'BillingApiError',
      body: stale,
    })
  })

  // covers: AC-19, AC-21
  it('aborts active writes and removes every private billing query on sign out', async () => {
    let requestSignal: AbortSignal | undefined
    client.post.mockImplementationOnce(
      (
        _path: string,
        options: { signal?: AbortSignal },
      ): Promise<{ error: { error: { code: string; message: string; request_id: string } } }> => {
        requestSignal = options.signal
        return new Promise((resolve) => {
          options.signal?.addEventListener('abort', () => {
            resolve({
              error: {
                error: {
                  code: 'cancelled',
                  message: 'request cancelled',
                  request_id: 'request-cancelled',
                },
              },
            })
          })
        })
      },
    )
    const { billingKeys, classRateKeys, clearBillingClientState, issueBillingPeriod } =
      await import('./billing')
    const queryClient = new QueryClient()
    queryClient.setQueryData(billingKeys.period('tutor-1', 2026, 8), { status: 'unissued' })
    queryClient.setQueryData(classRateKeys.detail('tutor-1', 'class-1'), { rates: [] })
    const issuing = issueBillingPeriod(2026, 8, 'preview-digest')

    await clearBillingClientState(queryClient)

    expect(requestSignal?.aborted).toBe(true)
    await expect(issuing).rejects.toMatchObject({ name: 'BillingApiError' })
    expect(queryClient.getQueryData(billingKeys.period('tutor-1', 2026, 8))).toBeUndefined()
    expect(queryClient.getQueryData(classRateKeys.detail('tutor-1', 'class-1'))).toBeUndefined()
  })
})
