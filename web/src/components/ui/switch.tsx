import * as SwitchPrimitive from '@radix-ui/react-switch'
import type { ComponentProps } from 'react'

import { cn } from '../../lib/utils'

export type SwitchProps = ComponentProps<typeof SwitchPrimitive.Root>

/** Pairs switch state with a moving thumb while preserving its accessible checked state. */
export function Switch({ className, ...props }: SwitchProps) {
  return (
    <SwitchPrimitive.Root
      className={cn(
        'inline-flex h-11 w-14 shrink-0 items-center rounded-full border border-border-strong bg-muted p-1 outline-none transition-[background-color,border-color,box-shadow] duration-base ease-standard motion-reduce:transition-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:border-primary data-[state=checked]:bg-primary',
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb className="block size-5 rounded-full bg-surface shadow-field transition-transform duration-base ease-standard motion-reduce:transition-none data-[state=checked]:translate-x-6" />
    </SwitchPrimitive.Root>
  )
}
