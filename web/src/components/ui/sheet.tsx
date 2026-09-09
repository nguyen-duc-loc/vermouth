import * as DialogPrimitive from '@radix-ui/react-dialog'
import { cva, type VariantProps } from 'class-variance-authority'
import { X } from 'lucide-react'
import type { ComponentProps } from 'react'

import { cn } from '../../lib/utils'

export const Sheet = DialogPrimitive.Root
export const SheetTrigger = DialogPrimitive.Trigger
export const SheetClose = DialogPrimitive.Close

const sheetVariants = cva(
  'fixed z-50 grid gap-4 overflow-y-auto border-border bg-surface-raised p-5 shadow-overlay outline-none sm:p-6',
  {
    variants: {
      side: {
        top: 'inset-x-0 top-0 max-h-[85dvh] border-b',
        bottom: 'inset-x-0 bottom-0 max-h-[85dvh] rounded-t-xl border-t',
        start: 'inset-y-0 start-0 h-full w-[min(24rem,90vw)] border-e',
        end: 'inset-y-0 end-0 h-full w-[min(24rem,90vw)] border-s',
      },
    },
    defaultVariants: { side: 'end' },
  },
)

export function SheetContent({
  side,
  className,
  children,
  closeLabel,
  ...props
}: ComponentProps<typeof DialogPrimitive.Content> &
  VariantProps<typeof sheetVariants> & { closeLabel: string }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-overlay" />
      <DialogPrimitive.Content className={cn(sheetVariants({ side }), className)} {...props}>
        {children}
        <DialogPrimitive.Close
          aria-label={closeLabel}
          className="absolute end-3 top-3 grid size-11 place-items-center rounded-md text-muted-foreground outline-none transition-colors duration-base hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background"
        >
          <X aria-hidden="true" className="size-icon-md" />
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

export function SheetHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('grid gap-2 pe-10', className)} {...props} />
}

export function SheetFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('mt-auto flex flex-col gap-3 sm:flex-row', className)} {...props} />
}

export function SheetTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return <DialogPrimitive.Title className={cn('text-xl font-semibold', className)} {...props} />
}

export function SheetDescription({
  className,
  ...props
}: ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn('text-sm leading-relaxed text-muted-foreground', className)}
      {...props}
    />
  )
}
