import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Circle, Home } from 'lucide-react'
import { describe, expect, it, vi } from 'vitest'

import { type AppDestination, AppShell } from './AppShell'
import { TooltipProvider } from './ui/tooltip'

vi.mock('@tanstack/react-router', () => ({
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: '/one' } }),
}))

const primary: readonly AppDestination[] = [
  { href: '/one', label: 'Một', icon: Home },
  { href: '/two', label: 'Hai', icon: Circle },
  { href: '/three', label: 'Ba', icon: Circle },
  { href: '/four', label: 'Bốn', icon: Circle },
  { href: '/five', label: 'Năm', icon: Circle },
  { href: '/six', label: 'Sáu', icon: Circle },
]

const secondary: readonly AppDestination[] = [
  { href: '/settings', label: 'Thiết lập', icon: Circle },
]

function renderShell() {
  return render(
    <TooltipProvider>
      <AppShell
        brandName="Vermouth"
        text={{
          skipToContent: 'Bỏ qua điều hướng',
          primaryNavigation: 'Điều hướng chính',
          moreActions: 'Thêm mục',
          account: 'Tài khoản và giao diện',
          accountDescription: 'Điều chỉnh giao diện trên thiết bị này.',
          closeAccount: 'Đóng bảng tài khoản',
        }}
        primaryDestinations={primary}
        secondaryDestinations={secondary}
        appearancePanel={<p>Bảng giao diện</p>}
        contextualPanel={<p>Ngữ cảnh hôm nay</p>}
      >
        <h1>Nội dung ngày dạy học</h1>
      </AppShell>
    </TooltipProvider>,
  )
}

describe('AppShell', () => {
  it('AC-6 limits phone navigation to five destinations and marks the current page', () => {
    renderShell()

    const navigations = screen.getAllByRole('navigation', { name: 'Điều hướng chính' })
    const phoneNavigation = navigations.at(-1)
    expect(phoneNavigation).toBeDefined()
    expect(within(phoneNavigation as HTMLElement).getAllByRole('link')).toHaveLength(5)
    expect(
      within(phoneNavigation as HTMLElement).getByRole('link', { name: 'Một' }),
    ).toHaveAttribute('aria-current', 'page')
  })

  it('AC-5 names every compact navigation link with caller supplied copy', () => {
    renderShell()

    const [wideNavigation] = screen.getAllByRole('navigation', { name: 'Điều hướng chính' })
    expect(wideNavigation).toBeDefined()

    for (const destination of primary) {
      expect(
        within(wideNavigation as HTMLElement).getByRole('link', { name: destination.label }),
      ).toHaveAttribute('href', destination.href)
    }
  })

  it('AC-6 keeps overflow and secondary destinations reachable from the phone menu', async () => {
    const user = userEvent.setup()
    renderShell()

    const menuTriggers = screen.getAllByRole('button', { name: 'Thêm mục' })
    await user.click(menuTriggers.at(-1) as HTMLElement)

    expect(screen.getByRole('menuitem', { name: 'Sáu' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Thiết lập' })).toBeInTheDocument()
  })

  it('AC-5 and AC-6 moves extra destinations into the zoom phone menu', async () => {
    const user = userEvent.setup()
    renderShell()

    const menuTriggers = screen.getAllByRole('button', { name: 'Thêm mục' })
    await user.click(menuTriggers.at(1) as HTMLElement)

    expect(screen.getByRole('menuitem', { name: 'Bốn' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Năm' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Sáu' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Thiết lập' })).toBeInTheDocument()
  })

  it('AC-5 opens the account sheet with caller copy and restores trigger focus', async () => {
    const user = userEvent.setup()
    renderShell()

    const accountTriggers = screen.getAllByRole('button', { name: 'Tài khoản và giao diện' })
    const trigger = accountTriggers.at(-1) as HTMLElement
    await user.click(trigger)

    expect(screen.getByRole('dialog', { name: 'Tài khoản và giao diện' })).toHaveTextContent(
      'Bảng giao diện',
    )
    await user.click(screen.getByRole('button', { name: 'Đóng bảng tài khoản' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('AC-5 exposes a skip link that targets the main content', () => {
    renderShell()

    expect(screen.getByRole('link', { name: 'Bỏ qua điều hướng' })).toHaveAttribute(
      'href',
      '#main-content',
    )
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main-content')
  })
})
