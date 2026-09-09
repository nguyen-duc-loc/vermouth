import { describe, expect, it } from 'vitest'

import { CLASS_COLORS, suggestClassColor } from './class-colors'

describe('suggestClassColor', () => {
  it('AC-10 returns blue for an empty identifier', () => {
    expect(suggestClassColor('')).toBe('blue')
  })

  it.each([
    ['a', 'yellow'],
    ['foobar', 'red'],
  ] as const)('AC-10 follows known FNV 1a vectors for %s', (classId, expected) => {
    expect(suggestClassColor(classId)).toBe(expected)
  })

  it('AC-10 is stable for repeated Unicode identifiers', () => {
    const classId = 'lớp học 7A · 018f8f7e-91b0-7cc4-bd8c-f4d9030ca421'

    expect(suggestClassColor(classId)).toBe(suggestClassColor(classId))
    expect(CLASS_COLORS).toContain(suggestClassColor(classId))
  })
})
