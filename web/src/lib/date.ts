import { format, type Locale } from 'date-fns'

/** Formats a pure date only when its caller supplies the locale and display pattern. */
export function formatLocalDate(date: Date, pattern: string, locale: Locale): string {
  return format(date, pattern, { locale })
}
