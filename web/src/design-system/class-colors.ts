import { ACCENT_CHOICES, type AccentChoice } from '../appearance/appearance'

export type ClassColor = AccentChoice

export const CLASS_COLORS = ACCENT_CHOICES

export const CLASS_COLOR_LABELS: Record<ClassColor, { vi: string; en: string }> = {
  red: { vi: 'Đỏ', en: 'Red' },
  rose: { vi: 'Hồng', en: 'Rose' },
  orange: { vi: 'Cam', en: 'Orange' },
  green: { vi: 'Xanh lá', en: 'Green' },
  blue: { vi: 'Xanh dương', en: 'Blue' },
  yellow: { vi: 'Vàng', en: 'Yellow' },
  violet: { vi: 'Tím', en: 'Violet' },
}

/** Suggests a stable class color from the UTF 8 bytes of its identifier. */
export function suggestClassColor(classId: string): ClassColor {
  if (!classId) return 'blue'

  let hash = 0x811c9dc5
  for (const byte of new TextEncoder().encode(classId)) {
    hash = Math.imul(hash ^ byte, 0x01000193) >>> 0
  }

  return CLASS_COLORS[hash % CLASS_COLORS.length] ?? 'blue'
}
