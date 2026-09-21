import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  authHeaders: vi.fn(),
  get: vi.fn(),
  put: vi.fn(),
  withProtectedRetry: vi.fn(),
}))

vi.mock('./client', () => ({
  api: { GET: client.get, PUT: client.put },
  authHeaders: client.authHeaders,
}))

vi.mock('./session', () => ({ withProtectedRetry: client.withProtectedRetry }))

beforeEach(() => {
  client.authHeaders.mockReturnValue({ Authorization: 'Bearer access-token' })
  client.withProtectedRetry.mockImplementation((request: () => Promise<unknown>) => request())
  client.get.mockReset()
  client.put.mockReset()
})

describe('private invoice profile API', () => {
  // covers: AC-2, AC-5, AC-14
  it('reads profile and banks through tutor scoped query keys and protected retry', async () => {
    const profile = invoiceProfile()
    const catalog = {
      banks: [
        {
          code: '970436',
          short_name: 'Vietcombank',
          official_name: 'Ngân hàng TMCP Ngoại Thương Việt Nam',
        },
      ],
    }
    client.get
      .mockResolvedValueOnce({ data: profile, response: new Response(null, { status: 200 }) })
      .mockResolvedValueOnce({ data: catalog, response: new Response(null, { status: 200 }) })
    const { bankKeys, profileKeys, readBanks, readInvoiceProfile } = await import('./profile')
    const profileSignal = new AbortController().signal
    const bankSignal = new AbortController().signal

    await expect(readInvoiceProfile(profileSignal)).resolves.toBe(profile)
    await expect(readBanks(bankSignal)).resolves.toBe(catalog)

    expect(profileKeys.detail('tutor-1')).toEqual(['invoice-profile', 'tutor-1'])
    expect(bankKeys.list('tutor-1')).toEqual(['banks', 'tutor-1'])
    expect(client.get).toHaveBeenNthCalledWith(1, '/api/invoice-profile', {
      headers: { Authorization: 'Bearer access-token' },
      signal: profileSignal,
    })
    expect(client.get).toHaveBeenNthCalledWith(2, '/api/banks', {
      headers: { Authorization: 'Bearer access-token' },
      signal: bankSignal,
    })
  })

  // covers: AC-3, AC-7, AC-11
  it('saves the whole generated request and preserves structured conflict details', async () => {
    const profile = invoiceProfile()
    const input = {
      expected_revision: 0,
      legal_name: 'Nguyễn An',
      contact_line: null,
      bank_code: null,
      bank_account_number: null,
      bank_account_holder: null,
    }
    client.put.mockResolvedValueOnce({
      data: profile,
      response: new Response(null, { status: 200 }),
    })
    const { putInvoiceProfile } = await import('./profile')

    await expect(putInvoiceProfile(input)).resolves.toBe(profile)
    expect(client.put).toHaveBeenCalledWith('/api/invoice-profile', {
      headers: { Authorization: 'Bearer access-token' },
      body: input,
      signal: expect.any(AbortSignal),
    })

    const conflict = {
      error: {
        code: 'profile_conflict',
        message: 'the profile changed after it was read',
        request_id: 'request-profile',
        details: { current_revision: 2 },
      },
    }
    client.put.mockResolvedValueOnce({
      error: conflict,
      response: new Response(null, { status: 409 }),
    })
    await expect(putInvoiceProfile(input)).rejects.toMatchObject({
      name: 'ProfileApiError',
      body: conflict,
    })
  })

  // covers: AC-14
  it('removes both private query families during sign out', async () => {
    const queryClient = new QueryClient()
    const { bankKeys, clearProfileClientState, profileKeys } = await import('./profile')
    queryClient.setQueryData(profileKeys.detail('tutor-1'), invoiceProfile())
    queryClient.setQueryData(bankKeys.list('tutor-1'), { banks: [] })

    await clearProfileClientState(queryClient)

    expect(queryClient.getQueryData(profileKeys.detail('tutor-1'))).toBeUndefined()
    expect(queryClient.getQueryData(bankKeys.list('tutor-1'))).toBeUndefined()
  })
})

function invoiceProfile() {
  return {
    legal_name: 'Nguyễn An',
    contact_line: null,
    bank_code: null,
    bank_name: null,
    bank_account_number: null,
    bank_account_holder: null,
    revision: 1,
    is_complete: false,
    missing_fields: ['contact_line', 'bank_code', 'bank_account_number', 'bank_account_holder'],
    bank_status: 'missing' as const,
  }
}
