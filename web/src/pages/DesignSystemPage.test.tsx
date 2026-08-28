import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AppearanceProvider } from '../appearance/appearance'
import { TooltipProvider } from '../components/ui/tooltip'
import { installMatchMedia } from '../test/setup'
import { DesignSystemPage } from './DesignSystemPage'

vi.mock('@tanstack/react-router', () => ({
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: '/design-system' } }),
}))

function renderGallery() {
  installMatchMedia(true)
  document.documentElement.dataset.themeChoice = 'system'
  document.documentElement.dataset.accent = 'blue'
  return render(
    <AppearanceProvider>
      <TooltipProvider delayDuration={0}>
        <DesignSystemPage />
      </TooltipProvider>
    </AppearanceProvider>,
  )
}

describe('DesignSystemPage', () => {
  it('AC-4 and AC-7 exposes the complete caller supplied component gallery', () => {
    renderGallery()

    expect(
      screen.getByRole('heading', { name: 'Một ngôn ngữ chung cho ngày dạy học' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: /Tên lớp học dùng để phân biệt/ })).toHaveValue(
      'Luyện thi Toán lớp 9',
    )
    expect(screen.getByRole('table', { name: /Danh sách mẫu/ })).toBeInTheDocument()
    expect(screen.getByRole('grid', { name: 'Lưới lịch học có hai chiều' })).toBeInTheDocument()
    expect(screen.getAllByRole('alert')).not.toHaveLength(0)
    expect(screen.getByText('Thursday, August 27, 2026')).toBeInTheDocument()
  })

  it('AC-5 moves through the true grid with arrow, Home, and End keys', async () => {
    const user = userEvent.setup()
    renderGallery()

    const monday = screen.getByRole('button', { name: 'Thứ hai, 07:30: Toán 8A' })
    monday.focus()
    await user.keyboard('{ArrowRight}')
    expect(screen.getByRole('button', { name: 'Thứ ba, 07:30: Trống' })).toHaveFocus()

    await user.keyboard('{End}')
    expect(screen.getByRole('button', { name: 'Chủ nhật, 07:30: Trống' })).toHaveFocus()

    await user.keyboard('{Home}')
    expect(monday).toHaveFocus()
  })

  it('AC-5 returns focus after the gallery dialog closes', async () => {
    const user = userEvent.setup()
    renderGallery()

    const trigger = screen.getByRole('button', { name: 'Mở hộp thoại' })
    await user.click(trigger)
    expect(screen.getByRole('dialog', { name: 'Xác nhận buổi học' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Đóng hộp thoại xác nhận' }))

    expect(screen.queryByRole('dialog', { name: 'Xác nhận buổi học' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('AC-4 changes gallery checkbox and switch states through their labels', async () => {
    const user = userEvent.setup()
    renderGallery()

    const attendance = screen.getByRole('checkbox', {
      name: 'Ghi học sinh này là có mặt trong buổi học hôm nay',
    })
    const notices = screen.getByRole('switch', { name: 'Thông báo thay đổi lịch' })
    await user.click(attendance)
    await user.click(notices)

    expect(attendance).not.toBeChecked()
    expect(notices).toBeChecked()
  })

  it('AC-6 and AC-8 demonstrates the ResponsiveTable empty rows branch', async () => {
    const user = userEvent.setup()
    renderGallery()

    await user.click(screen.getByRole('tab', { name: 'Chờ điểm danh' }))

    expect(
      screen.getByText('Không còn ai chờ điểm danh').closest('[data-responsive-table-state]'),
    ).toHaveAttribute('data-responsive-table-state', 'empty')
    expect(screen.queryByRole('table', { name: /Danh sách mẫu/ })).not.toBeInTheDocument()
  })
})
