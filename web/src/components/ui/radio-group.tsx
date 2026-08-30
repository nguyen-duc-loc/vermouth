import * as RadioGroupPrimitive from '@radix-ui/react-radio-group'
import type { ComponentProps, KeyboardEvent } from 'react'

import { cn } from '../../lib/utils'

export type RadioGroupProps = ComponentProps<typeof RadioGroupPrimitive.Root>
export type RadioGroupItemProps = ComponentProps<typeof RadioGroupPrimitive.Item>

const previousKeys = new Set(['ArrowLeft', 'ArrowUp'])
const nextKeys = new Set(['ArrowRight', 'ArrowDown'])

function selectRadioWithArrow(event: KeyboardEvent<HTMLDivElement>) {
  if (!previousKeys.has(event.key) && !nextKeys.has(event.key)) return

  const current = (event.target as HTMLElement).closest<HTMLElement>('[role="radio"]')
  if (!current) return

  const radios = Array.from(
    event.currentTarget.querySelectorAll<HTMLElement>(
      '[role="radio"]:not([disabled]):not([data-disabled])',
    ),
  )
  const currentIndex = radios.indexOf(current)
  if (currentIndex < 0) return

  const direction = previousKeys.has(event.key) ? -1 : 1
  const nextIndex = (currentIndex + direction + radios.length) % radios.length
  const next = radios[nextIndex]
  if (!next) return

  event.preventDefault()
  next.click()
  next.focus()
}

/** Groups exclusive choices under the Radix arrow key model. */
export function RadioGroup({ className, onKeyDownCapture, ...props }: RadioGroupProps) {
  return (
    <RadioGroupPrimitive.Root
      className={cn('grid gap-2', className)}
      onKeyDownCapture={(event) => {
        onKeyDownCapture?.(event)
        if (!event.defaultPrevented) selectRadioWithArrow(event)
      }}
      {...props}
    />
  )
}

/** Gives one radio choice a clear selected dot and a full phone target. */
export function RadioGroupItem({ className, children, ...props }: RadioGroupItemProps) {
  return (
    <RadioGroupPrimitive.Item
      className={cn(
        'grid size-11 shrink-0 place-items-center rounded-full border border-input bg-surface text-primary outline-none transition-[border-color,box-shadow] duration-base ease-standard motion-reduce:transition-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:border-primary',
        className,
      )}
      {...props}
    >
      <RadioGroupPrimitive.Indicator className="size-3 rounded-full bg-primary" />
      {children}
    </RadioGroupPrimitive.Item>
  )
}
