import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merges conditional classes without leaving conflicting Tailwind utilities behind. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
