import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps, ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ClassRates, PutClassRateResult } from '../api/billing'
import type { ClassRoster } from '../api/teaching'
import { AppearanceProvider } from '../appearance/appearance'
import { TooltipProvider } from '../components/ui/tooltip'
import { installMatchMedia } from '../test/setup'
import { ClassDetailPage } from './ClassDetailPage'

type MockLinkProps = Omit<ComponentProps<'a'>, 'href'> & { to: string; children: ReactNode }

const api = vi.hoisted(() => ({
  navigate: vi.fn(),
  putClassRate: vi.fn(),
  readClassRates: vi.fn(),
  readClassRoster: vi.fn(),
  readTutor: vi.fn(),
  search: { date: '2026-09-20', rateDate: undefined as string | undefined },
}))

vi.mock('@tanstack/react-router', () => ({
  getRouteApi: () => ({
    useNavigate: () => api.navigate,
    useParams: () => ({ classId: rates.class_id }),
    useSearch: () => api.search,
  }),
  Link: ({ to, children, ...props }: MockLinkProps) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  useNavigate: () => api.navigate,
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: `/classes/${rates.class_id}` } }),
}))

vi.mock('../api/billing', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/billing')>()
  return {
    ...original,
    putClassRate: api.putClassRate,
    readClassRates: api.readClassRates,
  }
})

vi.mock('../api/teaching', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/teaching')>()
  return {
    ...original,
    readClassRoster: api.readClassRoster,
    readTutor: api.readTutor,
  }
})

vi.mock('../components/AccountPanel', () => ({ AccountPanel: () => <div>Account</div> }))
vi.mock('../components/RosterManagementSheet', () => ({
  RosterManagementSheet: () => null,
}))

const roster: ClassRoster = {
  class: {
    class_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
    name: 'Maths 9A',
    color: 'blue',
  },
  resolved_date: '2026-09-20',
  students: [],
}

const rates: ClassRates = {
  class_id: roster.class.class_id,
  current: {
    rate_amount: 250_000,
    currency: 'VND',
    effective_from: '2026-09-01',
    rate_revision: 1,
  },
  allowed_range: { from: '2026-08-30', through: '2026-09-22' },
  archived: false,
  rates: [
    {
      effective_from: '2026-09-01',
      rate_amount: 250_000,
      currency: 'VND',
      rate_revision: 1,
    },
  ],
  projected_revision: 1,
  history_state: 'synced',
}

const savedRate: PutClassRateResult = {
  class_id: roster.class.class_id,
  effective_from: '2026-09-15',
  rate_amount: 300_000,
  currency: 'VND',
  rate_revision: 2,
  current: {
    rate_amount: 300_000,
    currency: 'VND',
    effective_from: '2026-09-15',
    rate_revision: 2,
  },
  allowed_range: rates.allowed_range,
  issued_invoices_unchanged: true,
  history_state: 'pending',
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <AppearanceProvider>
        <TooltipProvider>
          <ClassDetailPage />
        </TooltipProvider>
      </AppearanceProvider>
    </QueryClientProvider>,
  )
  return queryClient
}

beforeEach(() => {
  installMatchMedia(true)
  api.search.date = '2026-09-20'
  api.search.rateDate = undefined
  api.navigate.mockReset()
  api.putClassRate.mockReset().mockResolvedValue(savedRate)
  api.readClassRates.mockReset().mockResolvedValue(rates)
  api.readClassRoster.mockReset().mockResolvedValue(roster)
  api.readTutor.mockReset().mockResolvedValue({
    tutor_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422',
    email: 'tutor@example.com',
    display_name: 'Tutor',
    timezone: 'Asia/Ho_Chi_Minh',
    language: 'en',
    created_at: '2026-01-01T00:00:00Z',
  })
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('018f8f7e-91b0-7cc4-bd8c-f4d9030ca423')
})

describe('ClassDetailPage tuition rates', () => {
  // covers: AC-1, AC-3, AC-21, AC-22
  it('opens the dated rate editor with the server allowed range', async () => {
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Tuition rates' })).toBeVisible()
    expect(screen.getByText('From Sep 1, 2026 · revision 1')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Add or correct rate' }))

    expect(screen.getByRole('dialog', { name: 'Set a dated tuition rate' })).toBeVisible()
    expect(screen.getByLabelText('Effective date')).toHaveAttribute('min', '2026-08-30')
    expect(screen.getByLabelText('Effective date')).toHaveAttribute('max', '2026-09-22')
    expect(screen.getByLabelText('Effective date')).toHaveValue('2026-09-22')
    expect(screen.getByLabelText('Rate per Present session')).toHaveValue(250_000)
  })

  // covers: AC-3, AC-7, AC-22
  it.each([undefined, '2026-08-30'])(
    'corrects an archived class rate without an active roster, recovery date %s',
    async (recoveryDate) => {
      const user = userEvent.setup()
      api.search.rateDate = recoveryDate
      api.readClassRoster.mockRejectedValue(new Error('No active class matched this address.'))
      api.readClassRates.mockResolvedValue({
        ...rates,
        archived: true,
        allowed_range: { from: '2026-08-30', through: '2026-09-10' },
      } satisfies ClassRates)
      renderPage()

      if (!recoveryDate) {
        await user.click(await screen.findByRole('button', { name: 'Add or correct rate' }))
      }
      expect(await screen.findByRole('dialog', { name: 'Set a dated tuition rate' })).toBeVisible()
      expect(screen.getByLabelText('Effective date')).toHaveAttribute('min', '2026-08-30')
      expect(screen.getByLabelText('Effective date')).toHaveAttribute('max', '2026-09-10')
      expect(screen.getByLabelText('Effective date')).toHaveValue(recoveryDate ?? '2026-09-10')
      await user.clear(screen.getByLabelText('Rate per Present session'))
      await user.type(screen.getByLabelText('Rate per Present session'), '300000')
      await user.click(screen.getByRole('button', { name: 'Save dated rate' }))

      await waitFor(() =>
        expect(api.putClassRate).toHaveBeenCalledWith(
          rates.class_id,
          recoveryDate ?? '2026-09-10',
          { rate_amount: 300_000 },
          expect.any(String),
        ),
      )
      expect(screen.queryByRole('button', { name: 'Manage roster' })).not.toBeInTheDocument()
    },
  )

  // covers: AC-2, AC-21
  it('keeps one receipt key for retry and replaces it when the input changes', async () => {
    const user = userEvent.setup()
    api.putClassRate
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(savedRate)
    vi.spyOn(crypto, 'randomUUID')
      .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca423')
      .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca424')
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Add or correct rate' }))
    const amount = screen.getByLabelText('Rate per Present session')

    await user.click(screen.getByRole('button', { name: 'Save dated rate' }))
    expect(await screen.findByText('network unavailable')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Save dated rate' }))
    await waitFor(() => expect(api.putClassRate).toHaveBeenCalledTimes(2))

    await user.clear(amount)
    await user.type(amount, '300000')
    await user.click(screen.getByRole('button', { name: 'Save dated rate' }))
    await waitFor(() => expect(api.putClassRate).toHaveBeenCalledTimes(3))

    expect(api.putClassRate.mock.calls[0]?.[3]).toBe('018f8f7e-91b0-7cc4-bd8c-f4d9030ca423')
    expect(api.putClassRate.mock.calls[1]?.[3]).toBe('018f8f7e-91b0-7cc4-bd8c-f4d9030ca423')
    expect(api.putClassRate.mock.calls[2]?.[3]).toBe('018f8f7e-91b0-7cc4-bd8c-f4d9030ca424')
  })

  // covers: AC-2, AC-19, AC-21
  it('shows syncing until billing reaches the returned dated revision', async () => {
    const user = userEvent.setup()
    const queryClient = renderPage()
    await user.click(await screen.findByRole('button', { name: 'Add or correct rate' }))
    await user.clear(screen.getByLabelText('Effective date'))
    await user.type(screen.getByLabelText('Effective date'), '2026-09-15')
    await user.clear(screen.getByLabelText('Rate per Present session'))
    await user.type(screen.getByLabelText('Rate per Present session'), '300000')
    await user.click(screen.getByRole('button', { name: 'Save dated rate' }))

    expect(await screen.findByText('Rate saved. Billing history is syncing.')).toBeVisible()
    expect(screen.getByText('Syncing history')).toBeVisible()

    act(() => {
      queryClient.setQueryData(
        ['class-rates', '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422', rates.class_id],
        {
          ...rates,
          current: savedRate.current,
          projected_revision: 2,
          rates: [
            {
              effective_from: savedRate.effective_from,
              rate_amount: savedRate.rate_amount,
              currency: savedRate.currency,
              rate_revision: savedRate.rate_revision,
            },
            ...rates.rates,
          ],
        } satisfies ClassRates,
      )
    })

    expect(await screen.findByText('Rate history is synced with billing.')).toBeVisible()
    expect(screen.queryByText('Syncing history')).not.toBeInTheDocument()
  })

  // covers: AC-2, AC-19
  it('announces when a newer correction replaces the submitted dated rate', async () => {
    const user = userEvent.setup()
    const queryClient = renderPage()
    await user.click(await screen.findByRole('button', { name: 'Add or correct rate' }))
    await user.clear(screen.getByLabelText('Effective date'))
    await user.type(screen.getByLabelText('Effective date'), '2026-09-15')
    await user.clear(screen.getByLabelText('Rate per Present session'))
    await user.type(screen.getByLabelText('Rate per Present session'), '300000')
    await user.click(screen.getByRole('button', { name: 'Save dated rate' }))

    act(() => {
      queryClient.setQueryData(
        ['class-rates', '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422', rates.class_id],
        {
          ...rates,
          current: { ...savedRate.current, rate_revision: 3, rate_amount: 325_000 },
          projected_revision: 3,
          rates: [
            {
              effective_from: savedRate.effective_from,
              rate_amount: 325_000,
              currency: savedRate.currency,
              rate_revision: 3,
            },
            ...rates.rates,
          ],
        } satisfies ClassRates,
      )
    })

    expect(
      await screen.findByText('A newer correction replaced this rate in billing history.'),
    ).toBeVisible()
    expect(screen.queryByText('Syncing history')).not.toBeInTheDocument()
  })

  // covers: AC-19, AC-22
  it('restores server reported syncing after reload and polls until billing catches up', async () => {
    api.readClassRates
      .mockResolvedValueOnce({
        ...rates,
        projected_revision: 0,
        history_state: 'syncing',
      } satisfies ClassRates)
      .mockResolvedValueOnce(rates)
    renderPage()

    expect(await screen.findByText('Syncing history')).toBeVisible()
    await waitFor(() => expect(api.readClassRates).toHaveBeenCalledTimes(2), { timeout: 2_000 })
    await waitFor(() => expect(screen.queryByText('Syncing history')).not.toBeInTheDocument())
  })

  // covers: AC-19, AC-22
  it('keeps the confirmed command values visible when the history refresh fails', async () => {
    const user = userEvent.setup()
    api.readClassRates
      .mockResolvedValueOnce(rates)
      .mockRejectedValue(new Error('Billing unavailable'))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Add or correct rate' }))
    await user.clear(screen.getByLabelText('Effective date'))
    await user.type(screen.getByLabelText('Effective date'), '2026-09-15')
    await user.clear(screen.getByLabelText('Rate per Present session'))
    await user.type(screen.getByLabelText('Rate per Present session'), '300000')
    await user.click(screen.getByRole('button', { name: 'Save dated rate' }))

    expect(
      await screen.findByText(
        (_content, element) =>
          element?.tagName === 'P' &&
          element.textContent?.includes('300') === true &&
          element.textContent.includes('Sep 15, 2026'),
      ),
    ).toBeVisible()
    expect(screen.getByText('Rate saved. Billing history is syncing.')).toBeVisible()
  })

  // covers: AC-19, AC-22
  it('announces unavailable history from the server and polls for recovery', async () => {
    api.readClassRates
      .mockResolvedValueOnce({
        ...rates,
        rates: [],
        history_state: 'unavailable',
      } satisfies ClassRates)
      .mockResolvedValueOnce(rates)
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Billing rate history is unavailable. The confirmed teaching rate is still shown.',
    )
    await waitFor(() => expect(api.readClassRates).toHaveBeenCalledTimes(2), { timeout: 2_000 })
    await waitFor(() =>
      expect(
        screen.queryByText(
          'Billing rate history is unavailable. The confirmed teaching rate is still shown.',
        ),
      ).not.toBeInTheDocument(),
    )
  })

  // covers: AC-7, AC-19, AC-21
  it('opens recovery dates from billing and keeps teaching truth visible when history fails', async () => {
    const user = userEvent.setup()
    api.search.rateDate = '2026-08-30'
    api.readClassRates.mockRejectedValue(new Error('Billing history is unavailable'))
    renderPage()

    expect(await screen.findByRole('dialog', { name: 'Set a dated tuition rate' })).toBeVisible()
    expect(screen.getByLabelText('Effective date')).toHaveValue('2026-08-30')
    await user.click(screen.getByRole('button', { name: 'Close rate dialog' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Billing history is unavailable')
    expect(screen.getByRole('button', { name: 'Add or correct rate' })).toBeDisabled()
    expect(screen.getByRole('heading', { name: 'Maths 9A' })).toBeVisible()
  })
})
