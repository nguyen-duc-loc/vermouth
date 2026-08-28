import { Laptop, Moon, Sun } from 'lucide-react'

import {
  ACCENT_CHOICES,
  type AccentChoice,
  THEME_CHOICES,
  type ThemeChoice,
  useAppearance,
} from '../appearance/appearance'
import { cn } from '../lib/utils'
import { Label } from './ui/label'
import { RadioGroup, RadioGroupItem } from './ui/radio-group'

const themeIcons = {
  light: Sun,
  dark: Moon,
  system: Laptop,
} satisfies Record<ThemeChoice, typeof Sun>

export type AppearancePanelText = {
  title: string
  themeLegend: string
  accentLegend: string
  themes: Record<ThemeChoice, string>
  accents: Record<AccentChoice, string>
}

export type AppearancePanelProps = {
  text: AppearancePanelText
  className?: string
}

function isThemeChoice(value: string): value is ThemeChoice {
  return THEME_CHOICES.includes(value as ThemeChoice)
}

function isAccentChoice(value: string): value is AccentChoice {
  return ACCENT_CHOICES.includes(value as AccentChoice)
}

/** Lets callers place the same finite appearance controls in a menu or a settings page. */
export function AppearancePanel({ text, className }: AppearancePanelProps) {
  const { theme, accent, setTheme, setAccent } = useAppearance()

  return (
    <section className={cn('grid gap-5', className)} aria-labelledby="appearance-title">
      <h2 id="appearance-title" className="text-base font-semibold">
        {text.title}
      </h2>

      <fieldset className="grid gap-2">
        <legend className="mb-1 text-sm font-medium">{text.themeLegend}</legend>
        <RadioGroup
          value={theme}
          onValueChange={(value) => {
            if (isThemeChoice(value)) setTheme(value)
          }}
          className="grid grid-cols-3 gap-2"
        >
          {THEME_CHOICES.map((choice) => {
            const Icon = themeIcons[choice]
            const id = `appearance-theme-${choice}`
            return (
              <Label
                key={choice}
                htmlFor={id}
                className="flex min-w-0 cursor-pointer flex-col items-center gap-1 rounded-lg border border-border bg-surface p-2 text-center text-xs has-[[data-state=checked]]:border-primary has-[[data-state=checked]]:bg-primary/10"
              >
                <RadioGroupItem id={id} value={choice}>
                  <Icon aria-hidden="true" className="size-icon-sm" />
                </RadioGroupItem>
                <span className="w-full break-words">{text.themes[choice]}</span>
              </Label>
            )
          })}
        </RadioGroup>
      </fieldset>

      <fieldset className="grid gap-2">
        <legend className="mb-1 text-sm font-medium">{text.accentLegend}</legend>
        <RadioGroup
          value={accent}
          onValueChange={(value) => {
            if (isAccentChoice(value)) setAccent(value)
          }}
          className="grid grid-cols-4 gap-2 sm:grid-cols-7"
        >
          {ACCENT_CHOICES.map((choice) => {
            const id = `appearance-accent-${choice}`
            return (
              <Label
                key={choice}
                htmlFor={id}
                title={text.accents[choice]}
                className="flex min-w-0 cursor-pointer flex-col items-center gap-1 text-center text-xs"
              >
                <RadioGroupItem
                  id={id}
                  value={choice}
                  aria-label={text.accents[choice]}
                  data-class-color={choice}
                  className="border-class-marker bg-class-surface text-class-marker"
                />
                <span className="w-full break-words leading-tight">{text.accents[choice]}</span>
              </Label>
            )
          })}
        </RadioGroup>
      </fieldset>
    </section>
  )
}
