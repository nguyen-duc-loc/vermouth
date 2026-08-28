import {
  createContext,
  type PropsWithChildren,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'

export const THEME_CHOICES = ['light', 'dark', 'system'] as const
export const ACCENT_CHOICES = [
  'red',
  'rose',
  'orange',
  'green',
  'blue',
  'yellow',
  'violet',
] as const

export type ThemeChoice = (typeof THEME_CHOICES)[number]
export type AccentChoice = (typeof ACCENT_CHOICES)[number]

export type AppearancePreference = {
  theme: ThemeChoice
  accent: AccentChoice
}

type ResolvedTheme = Exclude<ThemeChoice, 'system'>

type AppearanceContextValue = AppearancePreference & {
  resolvedTheme: ResolvedTheme
  setTheme: (theme: ThemeChoice) => void
  setAccent: (accent: AccentChoice) => void
}

export const APPEARANCE_STORAGE_KEY = 'vermouth.appearance.v1'

const defaultPreference: AppearancePreference = {
  theme: 'system',
  accent: 'blue',
}

const AppearanceContext = createContext<AppearanceContextValue | null>(null)

function includesChoice<T extends string>(choices: readonly T[], value: unknown): value is T {
  return typeof value === 'string' && choices.includes(value as T)
}

/** Treats browser storage as optional and keeps only finite appearance values. */
export function parseAppearance(value: string | null): AppearancePreference {
  if (!value) return defaultPreference

  try {
    const parsed: unknown = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object') return defaultPreference

    const record = parsed as Record<string, unknown>
    return {
      theme: includesChoice(THEME_CHOICES, record.theme) ? record.theme : defaultPreference.theme,
      accent: includesChoice(ACCENT_CHOICES, record.accent)
        ? record.accent
        : defaultPreference.accent,
    }
  } catch {
    return defaultPreference
  }
}

function systemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function resolveTheme(theme: ThemeChoice): ResolvedTheme {
  return theme === 'system' ? systemTheme() : theme
}

function applyAppearance(preference: AppearancePreference) {
  const root = document.documentElement
  root.dataset.themeChoice = preference.theme
  root.dataset.theme = resolveTheme(preference.theme)
  root.dataset.accent = preference.accent
  root.style.colorScheme = root.dataset.theme
}

function initialPreference(): AppearancePreference {
  const root = document.documentElement
  return {
    theme: includesChoice(THEME_CHOICES, root.dataset.themeChoice)
      ? root.dataset.themeChoice
      : defaultPreference.theme,
    accent: includesChoice(ACCENT_CHOICES, root.dataset.accent)
      ? root.dataset.accent
      : defaultPreference.accent,
  }
}

/** Keeps appearance available even when browser storage is malformed or blocked. */
export function AppearanceProvider({ children }: PropsWithChildren) {
  const [preference, setPreference] = useState<AppearancePreference>(initialPreference)
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    resolveTheme(initialPreference().theme),
  )

  useEffect(() => {
    applyAppearance(preference)
    setResolvedTheme(resolveTheme(preference.theme))

    try {
      window.localStorage.setItem(APPEARANCE_STORAGE_KEY, JSON.stringify(preference))
    } catch {
      // Appearance remains valid in memory when storage is unavailable.
    }
  }, [preference])

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')

    const handleSystemChange = () => {
      if (preference.theme !== 'system') return
      applyAppearance(preference)
      setResolvedTheme(systemTheme())
    }

    media.addEventListener('change', handleSystemChange)
    return () => media.removeEventListener('change', handleSystemChange)
  }, [preference])

  useEffect(() => {
    const handleStorage = (event: StorageEvent) => {
      if (event.key !== APPEARANCE_STORAGE_KEY) return
      setPreference(parseAppearance(event.newValue))
    }

    window.addEventListener('storage', handleStorage)
    return () => window.removeEventListener('storage', handleStorage)
  }, [])

  const value = useMemo<AppearanceContextValue>(
    () => ({
      ...preference,
      resolvedTheme,
      setTheme: (theme) => setPreference((current) => ({ ...current, theme })),
      setAccent: (accent) => setPreference((current) => ({ ...current, accent })),
    }),
    [preference, resolvedTheme],
  )

  return <AppearanceContext.Provider value={value}>{children}</AppearanceContext.Provider>
}

/** Reads and changes the current finite appearance preference. */
export function useAppearance(): AppearanceContextValue {
  const value = useContext(AppearanceContext)
  if (!value) throw new Error('useAppearance requires AppearanceProvider')
  return value
}
