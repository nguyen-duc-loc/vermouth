import { useRouterState } from '@tanstack/react-router'
import { type LucideIcon, MoreHorizontal, UserRound } from 'lucide-react'
import type { ReactNode } from 'react'

import { cn } from '../lib/utils'
import { Button } from './ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from './ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'

export type AppDestination<TPath extends string = string> = {
  href: TPath
  label: string
  icon: LucideIcon
}

export type AppShellText = {
  skipToContent: string
  primaryNavigation: string
  moreActions: string
  account: string
  accountDescription: string
  closeAccount: string
}

export type AppShellProps<TPath extends string = string> = {
  brandName: string
  text: AppShellText
  primaryDestinations: readonly AppDestination<TPath>[]
  secondaryDestinations?: readonly AppDestination<TPath>[]
  appearancePanel: ReactNode
  contextualPanel?: ReactNode
  children: ReactNode
}

function NavigationLink({
  destination,
  active,
  compact = false,
}: {
  destination: AppDestination
  active: boolean
  compact?: boolean
}) {
  const Icon = destination.icon
  const link = (
    <a
      href={destination.href}
      aria-label={compact ? destination.label : undefined}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'group flex min-h-11 items-center rounded-lg text-sm font-medium text-muted-foreground outline-none transition-[color,background-color,box-shadow] duration-base ease-standard motion-reduce:transition-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background',
        active && 'bg-primary/10 text-primary-emphasis',
        compact ? 'size-11 justify-center' : 'gap-3 px-3',
      )}
    >
      <Icon aria-hidden="true" className="size-icon-md shrink-0" />
      {!compact && <span>{destination.label}</span>}
    </a>
  )

  if (!compact) return link

  return (
    <Tooltip>
      <TooltipTrigger asChild>{link}</TooltipTrigger>
      <TooltipContent side="right">{destination.label}</TooltipContent>
    </Tooltip>
  )
}

function MoreActions({
  label,
  destinations,
}: {
  label: string
  destinations: readonly AppDestination[]
}) {
  if (destinations.length === 0) return null

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="quiet" size="icon" aria-label={label}>
          <MoreHorizontal aria-hidden="true" className="size-icon-md" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{label}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {destinations.map((destination) => {
          const Icon = destination.icon
          return (
            <DropdownMenuItem key={destination.href} asChild>
              <a href={destination.href}>
                <Icon aria-hidden="true" className="size-icon-sm" />
                {destination.label}
              </a>
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** Adapts caller supplied destinations from a phone bar to a wide icon rail. */
export function AppShell<TPath extends string = string>({
  brandName,
  text,
  primaryDestinations,
  secondaryDestinations = [],
  appearancePanel,
  contextualPanel,
  children,
}: AppShellProps<TPath>) {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const phoneDestinations = primaryDestinations.slice(0, 5)
  const overflowDestinations = [...primaryDestinations.slice(5), ...secondaryDestinations]
  const zoomOverflowDestinations = [...primaryDestinations.slice(3), ...secondaryDestinations]

  const account = (
    <Sheet>
      <Tooltip>
        <TooltipTrigger asChild>
          <SheetTrigger asChild>
            <Button aria-label={text.account} size="icon" variant="quiet">
              <UserRound aria-hidden="true" className="size-icon-md" />
            </Button>
          </SheetTrigger>
        </TooltipTrigger>
        <TooltipContent>{text.account}</TooltipContent>
      </Tooltip>
      <SheetContent side="end" closeLabel={text.closeAccount}>
        <SheetHeader>
          <SheetTitle>{text.account}</SheetTitle>
          <SheetDescription>{text.accountDescription}</SheetDescription>
        </SheetHeader>
        <div className="mt-2">{appearancePanel}</div>
      </SheetContent>
    </Sheet>
  )

  return (
    <div className="min-h-screen bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only z-[100] rounded-md bg-surface font-medium shadow-overlay outline-none focus:not-sr-only focus:fixed focus:start-4 focus:top-4 focus:inline-flex focus:min-h-11 focus:items-center focus:px-4 focus:py-3 focus-visible:ring-2 focus-visible:ring-focus"
      >
        {text.skipToContent}
      </a>

      <aside className="fixed inset-y-0 start-0 z-40 hidden w-20 flex-col items-center border-e border-border bg-surface py-4 md:flex">
        <a
          href="/"
          aria-label={brandName}
          className="grid size-11 place-items-center rounded-xl bg-primary text-lg font-semibold text-primary-foreground outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background"
        >
          V
        </a>
        <nav aria-label={text.primaryNavigation} className="mt-8 grid gap-2">
          {primaryDestinations.map((destination) => (
            <NavigationLink
              key={destination.href}
              destination={destination}
              active={pathname === destination.href}
              compact
            />
          ))}
        </nav>
        <div className="mt-auto grid gap-2">
          <MoreActions label={text.moreActions} destinations={secondaryDestinations} />
          {account}
        </div>
      </aside>

      <header className="sticky top-0 z-30 flex min-h-16 items-center justify-between border-b border-border bg-background/95 px-4 backdrop-blur md:hidden">
        <a
          href="/"
          className="inline-flex min-h-11 min-w-0 items-center rounded-lg text-lg font-semibold tracking-tight wrap-anywhere outline-none focus-visible:ring-2 focus-visible:ring-focus"
        >
          {brandName}
        </a>
        <div className="flex items-center gap-1">
          <div className="xs:hidden">
            <MoreActions label={text.moreActions} destinations={zoomOverflowDestinations} />
          </div>
          <div className="hidden xs:block">
            <MoreActions label={text.moreActions} destinations={overflowDestinations} />
          </div>
          {account}
        </div>
      </header>

      <div
        className={cn('md:ps-20', contextualPanel && 'xl:grid xl:grid-cols-[minmax(0,1fr)_18rem]')}
      >
        <main id="main-content" tabIndex={-1} className="min-w-0 pb-28 outline-none md:pb-0">
          {children}
        </main>
        {contextualPanel && (
          <aside className="hidden border-s border-border bg-surface p-5 xl:block">
            {contextualPanel}
          </aside>
        )}
      </div>

      <nav
        aria-label={text.primaryNavigation}
        className="safe-area-pb fixed inset-x-0 bottom-0 z-40 flex min-h-16 items-stretch justify-around border-t border-border bg-surface/95 px-2 backdrop-blur md:hidden"
      >
        {phoneDestinations.map((destination, index) => {
          const Icon = destination.icon
          const active = pathname === destination.href
          return (
            <a
              key={destination.href}
              href={destination.href}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'flex min-h-14 min-w-11 flex-1 flex-col items-center justify-center gap-1 rounded-lg px-1 py-2 text-[0.6875rem] font-medium text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-inset',
                index >= 3 && 'hidden xs:flex',
                active && 'text-primary',
              )}
            >
              <Icon aria-hidden="true" className="size-icon-md" />
              <span className="max-w-full text-center leading-tight wrap-anywhere">
                {destination.label}
              </span>
            </a>
          )
        })}
      </nav>
    </div>
  )
}
