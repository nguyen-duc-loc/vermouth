import * as LabelPrimitive from '@radix-ui/react-label'
import type { ComponentProps } from 'react'

import { cn } from '../../lib/utils'

export type LabelProps = ComponentProps<typeof LabelPrimitive.Root>

/** Keeps labels selectable and visually consistent around native and Radix controls. */
export function Label({ className, ...props }: LabelProps) {
  return (
    <LabelPrimitive.Root
      className={cn(
        'min-w-0 text-sm font-medium leading-none text-foreground wrap-anywhere',
        className,
      )}
      {...props}
    />
  )
}
