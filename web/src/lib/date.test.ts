import { enUS, vi } from 'date-fns/locale'
import { describe, expect, it } from 'vitest'

import { formatLocalDate } from './date'

describe('formatLocalDate', () => {
  it('AC-7 uses the locale and pattern supplied by the caller', () => {
    const date = new Date(2026, 7, 27)

    expect(formatLocalDate(date, 'EEEE, d MMMM yyyy', vi)).toBe('Thứ Năm, 27 tháng 08 2026')
    expect(formatLocalDate(date, 'EEEE, MMMM d, yyyy', enUS)).toBe('Thursday, August 27, 2026')
  })
})
