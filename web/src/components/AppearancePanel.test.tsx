import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { AppearanceProvider } from '../appearance/appearance'
import { AppearancePanel, type AppearancePanelText } from './AppearancePanel'

const text: AppearancePanelText = {
  title: 'Giao diện của ngày dạy học',
  themeLegend: 'Chế độ sáng tối',
  accentLegend: 'Màu nhấn',
  themes: { light: 'Sáng', dark: 'Tối', system: 'Theo máy' },
  accents: {
    red: 'Đỏ',
    rose: 'Hồng',
    orange: 'Cam',
    green: 'Xanh lá',
    blue: 'Xanh dương',
    yellow: 'Vàng',
    violet: 'Tím',
  },
}

describe('AppearancePanel', () => {
  it('AC-5 keeps every theme radio at the shared 44 pixel target', () => {
    document.documentElement.dataset.themeChoice = 'system'
    document.documentElement.dataset.accent = 'blue'
    render(
      <AppearanceProvider>
        <AppearancePanel text={text} />
      </AppearanceProvider>,
    )

    for (const name of ['Sáng', 'Tối', 'Theo máy']) {
      expect(screen.getByRole('radio', { name })).toHaveClass('size-11')
    }
  })

  it('AC-2 and AC-7 exposes caller copy and changes both finite preferences', async () => {
    const user = userEvent.setup()
    document.documentElement.dataset.themeChoice = 'system'
    document.documentElement.dataset.accent = 'blue'
    render(
      <AppearanceProvider>
        <AppearancePanel text={text} />
      </AppearanceProvider>,
    )

    expect(screen.getByRole('heading', { name: text.title })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Theo máy' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'Xanh dương' })).toBeChecked()

    await user.click(screen.getByRole('radio', { name: 'Tối' }))
    await user.click(screen.getByRole('radio', { name: 'Hồng' }))

    expect(screen.getByRole('radio', { name: 'Tối' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'Hồng' })).toBeChecked()
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(document.documentElement).toHaveAttribute('data-accent', 'rose')
  })
})
