import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { installMatchMedia } from '../test/setup'
import {
  APPEARANCE_STORAGE_KEY,
  AppearanceProvider,
  parseAppearance,
  useAppearance,
} from './appearance'

// biome-ignore lint/style/useComponentExportOnlyModules: This test harness belongs only to this file.
function AppearanceProbe() {
  const appearance = useAppearance()
  return (
    <div>
      <output aria-label="theme choice">{appearance.theme}</output>
      <output aria-label="resolved theme">{appearance.resolvedTheme}</output>
      <output aria-label="accent choice">{appearance.accent}</output>
      <button type="button" onClick={() => appearance.setTheme('dark')}>
        Choose dark
      </button>
      <button type="button" onClick={() => appearance.setAccent('rose')}>
        Choose rose
      </button>
    </div>
  )
}

function renderAppearance() {
  return render(
    <AppearanceProvider>
      <AppearanceProbe />
    </AppearanceProvider>,
  )
}

describe('parseAppearance', () => {
  it.each([null, '', '{', 'null', '[]'])('AC-3 uses safe defaults for %s', (stored) => {
    expect(parseAppearance(stored)).toEqual({ theme: 'system', accent: 'blue' })
  })

  it('AC-3 validates each stored field independently', () => {
    expect(parseAppearance(JSON.stringify({ theme: 'dark', accent: 'unknown' }))).toEqual({
      theme: 'dark',
      accent: 'blue',
    })
  })
})

describe('AppearanceProvider', () => {
  beforeEach(() => {
    document.documentElement.dataset.themeChoice = 'system'
    document.documentElement.dataset.theme = 'light'
    document.documentElement.dataset.accent = 'blue'
  })

  it('AC-2 stores and applies choices made in the current tab', async () => {
    const user = userEvent.setup()
    renderAppearance()

    await user.click(screen.getByRole('button', { name: 'Choose dark' }))
    await user.click(screen.getByRole('button', { name: 'Choose rose' }))

    expect(screen.getByLabelText('theme choice')).toHaveTextContent('dark')
    expect(screen.getByLabelText('accent choice')).toHaveTextContent('rose')
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(document.documentElement).toHaveAttribute('data-accent', 'rose')
    expect(JSON.parse(window.localStorage.getItem(APPEARANCE_STORAGE_KEY) ?? '')).toEqual({
      theme: 'dark',
      accent: 'rose',
    })
  })

  it('AC-2 follows system theme changes only while system is selected', () => {
    const media = installMatchMedia(false)
    renderAppearance()

    expect(screen.getByLabelText('resolved theme')).toHaveTextContent('light')

    act(() => media.setMatches(true))

    expect(screen.getByLabelText('resolved theme')).toHaveTextContent('dark')
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
  })

  it('AC-2 synchronizes a valid preference from another tab', () => {
    renderAppearance()

    act(() => {
      window.dispatchEvent(
        new StorageEvent('storage', {
          key: APPEARANCE_STORAGE_KEY,
          newValue: JSON.stringify({ theme: 'dark', accent: 'green' }),
        }),
      )
    })

    expect(screen.getByLabelText('theme choice')).toHaveTextContent('dark')
    expect(screen.getByLabelText('accent choice')).toHaveTextContent('green')
  })

  it('AC-3 keeps current tab controls working when storage writes fail', async () => {
    const user = userEvent.setup()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    renderAppearance()

    await user.click(screen.getByRole('button', { name: 'Choose rose' }))

    expect(screen.getByLabelText('accent choice')).toHaveTextContent('rose')
    expect(document.documentElement).toHaveAttribute('data-accent', 'rose')
  })
})
