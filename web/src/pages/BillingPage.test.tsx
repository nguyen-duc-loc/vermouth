import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps, ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  type ApiError,
  BillingApiError,
  type BillingPeriodState,
  type BillingPreview,
  type BillingRun,
  billingKeys,
} from '../api/billing'
import { AppearanceProvider } from '../appearance/appearance'
import { TooltipProvider } from '../components/ui/tooltip'
import { BillingPage } from './BillingPage'

type MockLinkProps = Omit<ComponentProps<'a'>, 'href'> & {
  to: string
  children: ReactNode
  params?: Record<string, string>
  search?: Record<string, string | undefined>
}

const mocks = vi.hoisted(() => ({
  issueBillingPeriod: vi.fn(),
  navigate: vi.fn(),
  previewBillingPeriod: vi.fn(),
  readBillingPeriod: vi.fn(),
  readBillingPeriodDefault: vi.fn(),
  readTutor: vi.fn(),
  search: { year: 2026 as number | undefined, month: 8 as number | undefined },
}))

vi.mock('@tanstack/react-router', () => ({
  getRouteApi: () => ({
    useNavigate: () => mocks.navigate,
    useSearch: () => mocks.search,
  }),
  Link: ({ to, children, params, search, ...props }: MockLinkProps) => (
    <a
      href={to}
      data-params={JSON.stringify(params)}
      data-search={JSON.stringify(search)}
      {...props}
    >
      {children}
    </a>
  ),
  useNavigate: () => mocks.navigate,
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: '/billing' } }),
}))

vi.mock('../api/billing', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/billing')>()
  return {
    ...original,
    issueBillingPeriod: mocks.issueBillingPeriod,
    previewBillingPeriod: mocks.previewBillingPeriod,
    readBillingPeriod: mocks.readBillingPeriod,
    readBillingPeriodDefault: mocks.readBillingPeriodDefault,
  }
})

vi.mock('../api/teaching', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/teaching')>()
  return { ...original, readTutor: mocks.readTutor }
})

vi.mock('../components/AccountPanel', () => ({ AccountPanel: () => <div>Account</div> }))

const readyPreview: BillingPreview = {
  status: 'ready',
  period: { year: 2026, month: 8 },
  students: [
    {
      student_id: '01996765-8050-7000-8000-000000000001',
      student_name: 'Mai',
      lines: [
        {
          session_id: '01996765-8050-7000-8000-000000000002',
          class_id: '01996765-8050-7000-8000-000000000003',
          class_name: 'Maths 9A',
          local_date: '2026-08-18',
          rate_amount: 250_000,
          amount: 250_000,
          currency: 'VND',
        },
      ],
      total_amount: 250_000,
      currency: 'VND',
    },
  ],
  blockers: [],
  grand_total: 250_000,
  currency: 'VND',
  preview_fingerprint: 'fingerprint',
  run: null,
}

const issuedRun: BillingRun = {
  billing_run_id: '01996765-8050-7000-8000-000000000004',
  period: { year: 2026, month: 8 },
  generation: 1,
  created_at: '2026-09-01T00:00:00Z',
  invoices: [
    {
      invoice_id: '01996765-8050-7000-8000-000000000005',
      student_id: readyPreview.students[0].student_id,
      student_name: 'Mai',
      invoice_number: '2026-0001',
      total_amount: 250_000,
      currency: 'VND',
      issued_at: '2026-09-01T00:00:00Z',
      lines: readyPreview.students[0].lines,
    },
  ],
  grand_total: 250_000,
  currency: 'VND',
}

function deferred<T>() {
  let resolve: ((value: T) => void) | undefined
  let reject: ((error: Error) => void) | undefined
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return {
    promise,
    resolve: (value: T) => resolve?.(value),
    reject: (error: Error) => reject?.(error),
  }
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AppearanceProvider>
          <TooltipProvider>{children}</TooltipProvider>
        </AppearanceProvider>
      </QueryClientProvider>
    )
  }
  return { ...render(<BillingPage />, { wrapper: Wrapper }), queryClient }
}

beforeEach(() => {
  mocks.search.year = 2026
  mocks.search.month = 8
  mocks.navigate.mockReset()
  mocks.issueBillingPeriod.mockReset().mockResolvedValue(issuedRun)
  mocks.previewBillingPeriod.mockReset().mockResolvedValue(readyPreview)
  mocks.readBillingPeriod.mockReset().mockResolvedValue({
    status: 'unissued',
    period: { year: 2026, month: 8 },
    run: null,
  })
  mocks.readBillingPeriodDefault.mockReset().mockResolvedValue({
    server_date: '2026-09-22',
    timezone: 'Asia/Ho_Chi_Minh',
    year: 2026,
    month: 8,
    minimum_year: 2000,
  })
  mocks.readTutor.mockReset().mockResolvedValue({
    tutor_id: '01996765-8050-7000-8000-000000000010',
    email: 'tutor@example.com',
    display_name: 'Tutor',
    timezone: 'Asia/Ho_Chi_Minh',
    language: 'en',
    created_at: '2026-01-01T00:00:00Z',
  })
})

describe('BillingPage', () => {
  // covers: AC-22
  it('shows and retries an initial tutor read failure', async () => {
    const user = userEvent.setup()
    mocks.readTutor.mockRejectedValueOnce(new Error('Tutor could not be read'))
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'This billing month could not be opened' }),
    ).toBeVisible()
    expect(screen.getByText('Tutor could not be read')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('heading', { name: 'Student invoice review' })).toBeVisible()
    expect(mocks.readTutor).toHaveBeenCalledTimes(2)
  })

  // covers: AC-4, AC-22
  it('shows and retries a failed default month read when the URL has no month', async () => {
    const user = userEvent.setup()
    mocks.search.year = undefined
    mocks.search.month = undefined
    mocks.readBillingPeriodDefault.mockRejectedValueOnce(
      new Error('Default billing month could not be read'),
    )
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'This billing month could not be opened' }),
    ).toBeVisible()
    expect(screen.getByText('Default billing month could not be read')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    await waitFor(() => expect(mocks.readBillingPeriodDefault).toHaveBeenCalledTimes(2))
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: '/billing',
      search: { year: 2026, month: 8 },
      replace: true,
    })
  })

  // covers: AC-4, AC-22
  it('retries the failed default month prerequisite when the URL already has a month', async () => {
    const user = userEvent.setup()
    mocks.readBillingPeriodDefault.mockRejectedValueOnce(
      new Error('Default billing month could not be read'),
    )
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'This billing month could not be opened' }),
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('heading', { name: 'Student invoice review' })).toBeVisible()
    expect(mocks.readBillingPeriodDefault).toHaveBeenCalledTimes(2)
  })

  // covers: AC-4, AC-21
  it('replaces missing search values with the server completed month', async () => {
    mocks.search.year = undefined
    mocks.search.month = undefined
    renderPage()

    await waitFor(() => {
      expect(mocks.navigate).toHaveBeenCalledWith({
        to: '/billing',
        search: { year: 2026, month: 8 },
        replace: true,
      })
    })
    expect(screen.getByLabelText('Completed month')).toHaveAttribute('max', '2026-08')
  })

  // covers: AC-5, AC-6, AC-7, AC-22
  it('reviews Present lines and issues only from the displayed fingerprint', async () => {
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Student invoice review' })).toBeVisible()
    expect(screen.getByText('Maths 9A')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Issue 1 invoices' }))

    await waitFor(() => {
      expect(mocks.issueBillingPeriod).toHaveBeenCalledWith(2026, 8, 'fingerprint')
    })
    expect(await screen.findByRole('heading', { name: 'This month is issued' })).toBeVisible()
    expect(screen.getByText('2026-0001')).toBeVisible()
  })

  // covers: AC-17, AC-21, AC-22
  it('keeps a pending issue attached to its submitted month after navigation', async () => {
    const user = userEvent.setup()
    const response = deferred<BillingRun>()
    mocks.issueBillingPeriod.mockReturnValue(response.promise)
    mocks.readBillingPeriod.mockImplementation(async (year: number, month: number) => ({
      status: 'unissued',
      period: { year, month },
      run: null,
    }))
    mocks.previewBillingPeriod.mockImplementation(async (year: number, month: number) => ({
      ...readyPreview,
      period: { year, month },
      preview_fingerprint: `fingerprint-${month}`,
    }))
    const { queryClient, rerender } = renderPage()

    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))
    expect(mocks.issueBillingPeriod).toHaveBeenCalledWith(2026, 8, 'fingerprint-8')
    mocks.search.month = 7
    rerender(<BillingPage />)
    await waitFor(() =>
      expect(mocks.previewBillingPeriod).toHaveBeenCalledWith(2026, 7, expect.any(AbortSignal)),
    )

    await act(async () => response.resolve(issuedRun))

    const tutorId = '01996765-8050-7000-8000-000000000010'
    await waitFor(() => {
      expect(queryClient.getQueryData(billingKeys.period(tutorId, 2026, 8))).toMatchObject({
        status: 'already_issued',
        run: issuedRun,
      })
    })
    expect(queryClient.getQueryData(billingKeys.period(tutorId, 2026, 7))).toMatchObject({
      status: 'unissued',
      run: null,
    })
    expect(queryClient.getQueryData(billingKeys.preview(tutorId, 2026, 7))).toMatchObject({
      preview_fingerprint: 'fingerprint-7',
    })
    expect(screen.getByLabelText('Completed month')).toHaveValue('2026-07')
    expect(screen.queryByText('2026-0001')).not.toBeInTheDocument()
  })

  it('keeps issue disabled and gives a typed recovery link while blocked', async () => {
    mocks.previewBillingPeriod.mockResolvedValue({
      ...readyPreview,
      status: 'blocked',
      students: [],
      grand_total: 0,
      preview_fingerprint: null,
      blockers: [
        {
          code: 'profile_incomplete',
          student_id: null,
          session_id: null,
          class_id: null,
          local_date: null,
          field: 'bank_code',
          destination: {
            route: '/profile',
            date: null,
            session_id: null,
            class_id: null,
            rate_date: null,
          },
        },
      ],
    })
    renderPage()

    expect(await screen.findByText('Complete profile field: bank_code')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Issue 0 invoices' })).toBeDisabled()
    expect(screen.getByRole('link', { name: /Complete profile field/ })).toHaveAttribute(
      'href',
      '/profile',
    )
  })

  // covers: AC-7, AC-21, AC-22
  it('keeps attendance and missing rate recovery destinations distinct', async () => {
    mocks.previewBillingPeriod.mockResolvedValue({
      ...readyPreview,
      status: 'blocked',
      preview_fingerprint: null,
      blockers: [
        {
          code: 'attendance_incomplete',
          student_id: readyPreview.students[0].student_id,
          session_id: readyPreview.students[0].lines[0].session_id,
          class_id: readyPreview.students[0].lines[0].class_id,
          local_date: '2026-08-18',
          field: null,
          destination: {
            route: '/',
            date: '2026-08-18',
            session_id: readyPreview.students[0].lines[0].session_id,
            class_id: null,
            rate_date: null,
          },
        },
        {
          code: 'rate_missing',
          student_id: readyPreview.students[0].student_id,
          session_id: readyPreview.students[0].lines[0].session_id,
          class_id: readyPreview.students[0].lines[0].class_id,
          local_date: '2026-08-18',
          field: null,
          destination: {
            route: `/classes/${readyPreview.students[0].lines[0].class_id}`,
            date: null,
            session_id: null,
            class_id: readyPreview.students[0].lines[0].class_id,
            rate_date: '2026-08-18',
          },
        },
      ],
    })
    renderPage()

    const attendance = await screen.findByRole('link', { name: /Mark attendance/ })
    const rate = screen.getByRole('link', { name: /Add a rate/ })
    expect(attendance).toHaveAttribute(
      'data-search',
      JSON.stringify({
        date: '2026-08-18',
        session: readyPreview.students[0].lines[0].session_id,
      }),
    )
    expect(rate).toHaveAttribute(
      'data-search',
      JSON.stringify({ date: undefined, rateDate: '2026-08-18' }),
    )
    expect(screen.getByRole('button', { name: 'Issue 1 invoices' })).toBeDisabled()
  })

  // covers: AC-7, AC-21
  it('shows an honest empty month without offering issue', async () => {
    mocks.previewBillingPeriod.mockResolvedValue({
      ...readyPreview,
      status: 'empty',
      students: [],
      blockers: [],
      grand_total: 0,
      preview_fingerprint: null,
    })
    renderPage()

    expect(await screen.findByText('No invoices for this month')).toBeVisible()
    expect(screen.queryByRole('button', { name: /Issue/ })).not.toBeInTheDocument()
  })

  // covers: AC-13, AC-17, AC-19, AC-21
  it('requires a fresh review when issue reports a stale preview', async () => {
    const user = userEvent.setup()
    const stale: ApiError = {
      error: {
        code: 'preview_stale',
        message: 'the billing inputs changed after preview',
        request_id: 'request-billing',
      },
    }
    mocks.issueBillingPeriod.mockRejectedValue(
      new BillingApiError(stale, 'the billing month could not be issued'),
    )
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))

    expect(
      await screen.findByText(
        'Teaching facts changed. Review the refreshed preview before issuing.',
      ),
    ).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'This month is issued' })).not.toBeInTheDocument()
  })

  // covers: AC-17, AC-19, AC-21, AC-22
  it('reads the authoritative period after a lost issue response before offering issue again', async () => {
    const user = userEvent.setup()
    const recovery = deferred<BillingPeriodState>()
    mocks.readBillingPeriod
      .mockResolvedValueOnce({ status: 'unissued', period: readyPreview.period, run: null })
      .mockReturnValueOnce(recovery.promise)
    mocks.issueBillingPeriod.mockRejectedValue(new Error('The issue response was lost'))
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))

    await waitFor(() => expect(mocks.readBillingPeriod).toHaveBeenCalledTimes(2))
    expect(mocks.readBillingPeriod).toHaveBeenLastCalledWith(2026, 8, expect.any(AbortSignal))
    expect(screen.getByRole('button', { name: 'Issue 1 invoices' })).toBeDisabled()
    await act(async () =>
      recovery.resolve({
        status: 'already_issued',
        period: issuedRun.period,
        run: issuedRun,
      }),
    )

    expect(await screen.findByRole('heading', { name: 'This month is issued' })).toBeVisible()
    expect(screen.getByText('2026-0001')).toBeVisible()
    expect(mocks.issueBillingPeriod).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: /Issue/ })).not.toBeInTheDocument()
  })

  // covers: AC-17, AC-22
  it('offers a new explicit issue only after recovery confirms the month is unissued', async () => {
    const user = userEvent.setup()
    const recovery = deferred<BillingPeriodState>()
    mocks.readBillingPeriod
      .mockResolvedValueOnce({ status: 'unissued', period: readyPreview.period, run: null })
      .mockReturnValueOnce(recovery.promise)
    mocks.issueBillingPeriod.mockRejectedValueOnce(new Error('The issue response was lost'))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))

    await waitFor(() => expect(mocks.readBillingPeriod).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: 'Issue 1 invoices' })).toBeDisabled()
    await act(async () =>
      recovery.resolve({
        status: 'unissued',
        period: readyPreview.period,
        run: null,
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Issue 1 invoices' })).toBeEnabled(),
    )
    expect(mocks.issueBillingPeriod).toHaveBeenCalledTimes(1)
    await user.click(screen.getByRole('button', { name: 'Issue 1 invoices' }))
    expect(await screen.findByRole('heading', { name: 'This month is issued' })).toBeVisible()
    expect(mocks.issueBillingPeriod).toHaveBeenCalledTimes(2)
  })

  // covers: AC-17, AC-22
  it('keeps issue unavailable when the authoritative recovery read fails', async () => {
    const user = userEvent.setup()
    mocks.readBillingPeriod
      .mockResolvedValueOnce({ status: 'unissued', period: readyPreview.period, run: null })
      .mockRejectedValueOnce(new Error('The billing month could not be read'))
    mocks.issueBillingPeriod.mockRejectedValue(new Error('The issue response was lost'))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))

    expect(
      await screen.findByRole('heading', { name: 'This billing month could not be opened' }),
    ).toBeVisible()
    expect(screen.queryByRole('button', { name: /Issue/ })).not.toBeInTheDocument()
    expect(mocks.issueBillingPeriod).toHaveBeenCalledTimes(1)
  })

  // covers: AC-17, AC-21, AC-22
  it('recovers the submitted month after an uncertain response even after navigation', async () => {
    const user = userEvent.setup()
    const response = deferred<BillingRun>()
    mocks.issueBillingPeriod.mockReturnValue(response.promise)
    mocks.readBillingPeriod.mockImplementation(async (year: number, month: number) => ({
      status: 'unissued',
      period: { year, month },
      run: null,
    }))
    mocks.previewBillingPeriod.mockImplementation(async (year: number, month: number) => ({
      ...readyPreview,
      period: { year, month },
    }))
    const { queryClient, rerender } = renderPage()
    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))
    mocks.search.month = 7
    rerender(<BillingPage />)
    await waitFor(() =>
      expect(mocks.previewBillingPeriod).toHaveBeenCalledWith(2026, 7, expect.any(AbortSignal)),
    )
    mocks.readBillingPeriod.mockResolvedValueOnce({
      status: 'already_issued',
      period: issuedRun.period,
      run: issuedRun,
    })

    await act(async () => response.reject(new Error('The issue response was lost')))

    await waitFor(() =>
      expect(mocks.readBillingPeriod).toHaveBeenLastCalledWith(2026, 8, expect.any(AbortSignal)),
    )
    expect(
      queryClient.getQueryData(billingKeys.period('01996765-8050-7000-8000-000000000010', 2026, 8)),
    ).toMatchObject({ run: issuedRun })
    expect(screen.getByLabelText('Completed month')).toHaveValue('2026-07')
    expect(screen.queryByText('2026-0001')).not.toBeInTheDocument()
  })

  // covers: AC-21
  it('does not start recovery after the issue request is cancelled for sign out', async () => {
    const user = userEvent.setup()
    mocks.issueBillingPeriod.mockRejectedValue(new DOMException('Request cancelled', 'AbortError'))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))

    expect(await screen.findByText('Request cancelled')).toBeVisible()
    expect(mocks.readBillingPeriod).toHaveBeenCalledTimes(1)
  })

  // covers: AC-17, AC-21, AC-22
  it('keeps issue disabled when navigation restarts a cancelled recovery read', async () => {
    const user = userEvent.setup()
    const returningRead = deferred<BillingPeriodState>()
    let recoverySignal: AbortSignal | undefined
    let augustReads = 0
    mocks.readBillingPeriod.mockImplementation(
      (year: number, month: number, signal: AbortSignal) => {
        if (month === 8) {
          augustReads += 1
          if (augustReads === 2) {
            recoverySignal = signal
            return new Promise<BillingPeriodState>((_resolve, reject) => {
              signal.addEventListener('abort', () =>
                reject(new DOMException('Request cancelled', 'AbortError')),
              )
            })
          }
          if (augustReads === 3) return returningRead.promise
        }
        return Promise.resolve({ status: 'unissued', period: { year, month }, run: null })
      },
    )
    mocks.previewBillingPeriod.mockImplementation(async (year: number, month: number) => ({
      ...readyPreview,
      period: { year, month },
    }))
    mocks.issueBillingPeriod.mockRejectedValue(new Error('The issue response was lost'))
    const { rerender } = renderPage()
    await user.click(await screen.findByRole('button', { name: 'Issue 1 invoices' }))
    await waitFor(() => expect(augustReads).toBe(2))

    mocks.search.month = 7
    rerender(<BillingPage />)
    await waitFor(() => expect(recoverySignal?.aborted).toBe(true))
    await screen.findByRole('heading', { name: 'Student invoice review' })
    mocks.search.month = 8
    rerender(<BillingPage />)

    await waitFor(() => expect(augustReads).toBe(3))
    expect(screen.getByRole('button', { name: 'Issue 1 invoices' })).toBeDisabled()
    await act(async () =>
      returningRead.resolve({
        status: 'already_issued',
        period: issuedRun.period,
        run: issuedRun,
      }),
    )
    expect(await screen.findByRole('heading', { name: 'This month is issued' })).toBeVisible()
    expect(mocks.issueBillingPeriod).toHaveBeenCalledTimes(1)
  })

  // covers: AC-18, AC-21
  it('shows the committed run when preview discovers a concurrent issue', async () => {
    mocks.previewBillingPeriod.mockResolvedValue({
      ...readyPreview,
      status: 'already_issued',
      students: [],
      preview_fingerprint: null,
      run: issuedRun,
    } satisfies BillingPreview)
    const { queryClient } = renderPage()

    expect(await screen.findByRole('heading', { name: 'This month is issued' })).toBeVisible()
    expect(screen.getByText('2026-0001')).toBeVisible()
    expect(screen.queryByRole('button', { name: /Issue/ })).not.toBeInTheDocument()
    expect(
      queryClient.getQueryData(billingKeys.period('01996765-8050-7000-8000-000000000010', 2026, 8)),
    ).toMatchObject({
      status: 'already_issued',
      run: issuedRun,
    })
    expect(mocks.issueBillingPeriod).not.toHaveBeenCalled()
  })
})
