import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { installMatchMedia } from '../test/setup'
import { PageEntrance } from './PageEntrance'

describe('PageEntrance', () => {
  it('AC-9 renders reduced motion content immediately in its final state', () => {
    installMatchMedia(true)

    render(
      <PageEntrance>
        <p data-entrance-item>Nội dung sẵn sàng</p>
      </PageEntrance>,
    )

    expect(screen.getByText('Nội dung sẵn sàng')).toBeVisible()
  })

  it('does not warn while a page has no entrance targets yet', () => {
    installMatchMedia(true)
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => undefined)

    render(
      <PageEntrance>
        <p>Loading the record</p>
      </PageEntrance>,
    )

    expect(warning).not.toHaveBeenCalled()
    warning.mockRestore()
  })
})
