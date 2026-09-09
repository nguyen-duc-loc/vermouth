import type { ReactNode } from 'react'

import { Button, type ButtonProps } from './ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'

export type IconButtonProps = Omit<ButtonProps, 'children' | 'size'> & {
  label: string
  icon: ReactNode
}

/** Gives an icon action both a spoken name and a visible pointer label. */
export function IconButton({ label, icon, ...props }: IconButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button aria-label={label} size="icon" variant="quiet" {...props}>
          {icon}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
