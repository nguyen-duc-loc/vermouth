import { Toaster as Sonner, toast } from 'sonner'

import { useAppearance } from '../../appearance/appearance'

export { toast }

/** Places caller supplied toast copy on the current light or dark surface. */
export function Toaster() {
  const { resolvedTheme } = useAppearance()
  return (
    <Sonner
      theme={resolvedTheme}
      position="top-right"
      toastOptions={{
        classNames: {
          toast: 'border-border bg-surface-raised text-foreground shadow-overlay',
          description: 'text-muted-foreground',
          actionButton: 'bg-primary text-primary-foreground',
        },
      }}
    />
  )
}
