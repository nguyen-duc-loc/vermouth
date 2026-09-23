import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  putProfile: vi.fn(),
  readBanks: vi.fn(),
  readProfile: vi.fn(),
  readTutor: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useBlocker: () => ({ status: 'idle' }),
}))

vi.mock('../api/profile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/profile')>()
  return {
    ...actual,
    putInvoiceProfile: api.putProfile,
    readBanks: api.readBanks,
    readInvoiceProfile: api.readProfile,
  }
})

vi.mock('../api/teaching', () => ({ readTutor: api.readTutor }))
vi.mock('../components/AccountPanel', () => ({ AccountPanel: () => <p>Account panel</p> }))
vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: ReactNode }) => <main>{children}</main>,
}))
vi.mock('../components/PageEntrance', () => ({
  PageEntrance: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))
vi.mock('../components/ui/sonner', () => ({ toast: { success: vi.fn() } }))

import { ProfilePage } from './ProfilePage'

beforeEach(() => {
  api.putProfile.mockReset()
  api.readBanks.mockReset()
  api.readProfile.mockReset()
  api.readTutor.mockReset()
  api.readTutor.mockResolvedValue({ tutor_id: 'tutor-1' })
  api.readBanks.mockResolvedValue({
    banks: [
      {
        code: '970436',
        short_name: 'Vietcombank',
        official_name: 'Ngân hàng TMCP Ngoại Thương Việt Nam',
      },
    ],
  })
})

describe('ProfilePage', () => {
  // covers: AC-1, AC-3, AC-5, AC-6, AC-10
  it('searches Vietnamese bank names and saves the normalized whole profile', async () => {
    const user = userEvent.setup()
    api.readProfile.mockResolvedValue(emptyProfile())
    api.putProfile.mockImplementation(async (input) => ({
      ...emptyProfile(),
      legal_name: input.legal_name,
      revision: 1,
      missing_fields: ['contact_line', 'bank_code', 'bank_account_number', 'bank_account_holder'],
    }))
    renderProfile()

    const legalName = await screen.findByRole('textbox', { name: 'Legal invoice name' })
    await user.type(legalName, '  Nguyễn   An  ')
    await user.type(screen.getByRole('searchbox', { name: 'Search banks' }), 'ngoai thuong')

    expect(screen.getByRole('radio', { name: /Vietcombank/ })).toBeInTheDocument()
    const save = screen.getByRole('button', { name: 'Save profile' })
    await user.click(save)

    await waitFor(() =>
      expect(api.putProfile).toHaveBeenCalledWith({
        expected_revision: 0,
        legal_name: 'Nguyễn An',
        contact_line: null,
        bank_code: null,
        bank_account_number: null,
        bank_account_holder: null,
      }),
    )
    expect(await screen.findByText('Profile saved.')).toBeInTheDocument()
  })

  // covers: AC-11
  it('shows a blocking retry state instead of an empty form when profile loading fails', async () => {
    const user = userEvent.setup()
    api.readProfile.mockRejectedValue(new Error('offline'))
    renderProfile()

    expect(
      await screen.findByRole('heading', { name: 'Your profile could not be loaded' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Legal invoice name' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(api.readProfile).toHaveBeenCalledTimes(2)
  })

  // covers: AC-12
  it('names cleared fields before a complete profile can be saved incomplete', async () => {
    const user = userEvent.setup()
    const complete = completeProfile()
    api.readProfile.mockResolvedValue(complete)
    api.putProfile.mockResolvedValue({
      ...complete,
      contact_line: null,
      revision: 3,
      is_complete: false,
      missing_fields: ['contact_line'],
    })
    renderProfile()

    const contact = await screen.findByRole('textbox', { name: 'Invoice contact' })
    await user.clear(contact)
    const save = screen.getByRole('button', { name: 'Save profile' })
    await user.click(save)

    const dialog = screen.getByRole('dialog', { name: 'Save an incomplete profile?' })
    expect(dialog).toHaveTextContent('Invoice contact')
    expect(api.putProfile).not.toHaveBeenCalled()
    await user.keyboard('{Escape}')
    expect(save).toHaveFocus()
    await user.click(save)
    await user.click(screen.getByRole('button', { name: 'Save incomplete profile' }))
    await waitFor(() => expect(api.putProfile).toHaveBeenCalledOnce())
  })
})

function renderProfile() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <ProfilePage />
    </QueryClientProvider>,
  )
}

function emptyProfile() {
  return {
    legal_name: null,
    contact_line: null,
    bank_code: null,
    bank_name: null,
    bank_account_number: null,
    bank_account_holder: null,
    revision: 0,
    is_complete: false,
    missing_fields: [
      'legal_name',
      'contact_line',
      'bank_code',
      'bank_account_number',
      'bank_account_holder',
    ],
    bank_status: 'missing' as const,
  }
}

function completeProfile() {
  return {
    legal_name: 'Nguyễn An',
    contact_line: 'invoices@example.com',
    bank_code: '970436',
    bank_name: 'Vietcombank',
    bank_account_number: 'AB123',
    bank_account_holder: 'NGUYỄN AN',
    revision: 2,
    is_complete: true,
    missing_fields: [],
    bank_status: 'active' as const,
  }
}
