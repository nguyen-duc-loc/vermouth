import { useGSAP } from '@gsap/react'
import gsap from 'gsap'
import { type PropsWithChildren, useRef } from 'react'

import { cn } from '../lib/utils'

gsap.registerPlugin(useGSAP)

function durationSeconds(token: string): number {
  const value = getComputedStyle(document.documentElement).getPropertyValue(token).trim()
  if (value.endsWith('ms')) return Number.parseFloat(value) / 1000
  if (value.endsWith('s')) return Number.parseFloat(value)
  return 0
}

export type PageEntranceProps = PropsWithChildren<{ className?: string }>

/** Coordinates one restrained entrance and leaves reduced motion content immediately usable. */
export function PageEntrance({ children, className }: PageEntranceProps) {
  const scope = useRef<HTMLDivElement>(null)

  useGSAP(
    () => {
      const media = gsap.matchMedia()
      media.add(
        {
          reduceMotion: '(prefers-reduced-motion: reduce)',
          allowMotion: '(prefers-reduced-motion: no-preference)',
        },
        (context) => {
          if (context.conditions?.reduceMotion) {
            gsap.set('[data-entrance-item]', { autoAlpha: 1, y: 0 })
            return
          }

          gsap.fromTo(
            '[data-entrance-item]',
            { autoAlpha: 0, y: 12 },
            {
              autoAlpha: 1,
              y: 0,
              duration: durationSeconds('--duration-slow'),
              ease: 'power2.out',
              stagger: 0.045,
              clearProps: 'opacity,visibility,transform',
            },
          )
        },
        scope,
      )

      return () => media.revert()
    },
    { scope },
  )

  return (
    <div ref={scope} className={cn('min-w-0 grid-cols-[minmax(0,1fr)]', className)}>
      {children}
    </div>
  )
}
