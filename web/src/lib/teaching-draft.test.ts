import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  clearAllTeachingDrafts,
  clearTeachingDraft,
  loadTeachingDraft,
  newTeachingDraft,
  saveTeachingDraft,
} from './teaching-draft'

const defaults = {
  local_date: '2026-08-30',
  start_time: '10:00',
  end_time: '11:00',
}

beforeEach(() => {
  window.sessionStorage.clear()
  vi.spyOn(crypto, 'randomUUID')
    .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca421')
    .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca422')
})

describe('teaching draft storage', () => {
  // covers: AC-1, AC-10, AC-14
  it('starts from server defaults with one stable command key per create step', () => {
    const draft = newTeachingDraft('tutor-1', defaults)

    expect(draft).toEqual({
      tutorId: 'tutor-1',
      step: 'class',
      classKey: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
      studentKey: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422',
      values: {
        className: '',
        rateAmount: '',
        color: 'suggest',
        localDate: '2026-08-30',
        startTime: '10:00',
        endTime: '11:00',
        studentName: '',
        phone: '',
      },
    })
  })

  // covers: AC-10, AC-14
  it('restores the same tutor step values resources and command keys', () => {
    const draft = {
      ...newTeachingDraft('tutor-1', defaults),
      step: 'roster' as const,
      classId: 'class-1',
      sessionId: 'session-1',
      studentId: 'student-1',
      firstLocalDate: '2026-08-30',
      values: {
        ...newTeachingDraft('unused', defaults).values,
        className: 'Maths 9A',
        studentName: 'Mai',
        phone: '0901234567',
      },
    }

    saveTeachingDraft(draft)

    expect(loadTeachingDraft('tutor-1')).toEqual(draft)
  })

  // covers: AC-6, AC-14
  it('clears another tutor draft before loading the current account', () => {
    const first = newTeachingDraft('tutor-1', defaults)
    const second = { ...first, tutorId: 'tutor-2' }
    saveTeachingDraft(first)
    saveTeachingDraft(second)

    expect(loadTeachingDraft('tutor-2')).toEqual(second)
    expect(window.sessionStorage.getItem('vermouth.teaching-setup.v1:tutor-1')).toBeNull()
  })

  // covers: AC-6, AC-14
  it.each([
    ['invalid JSON', '{'],
    ['wrong tutor', JSON.stringify({ tutorId: 'tutor-2', step: 'class', values: {} })],
    ['unknown step', JSON.stringify({ tutorId: 'tutor-1', step: 'done', values: {} })],
    ['missing values', JSON.stringify({ tutorId: 'tutor-1', step: 'class' })],
  ])('removes %s instead of trusting it', (_name, value) => {
    const key = 'vermouth.teaching-setup.v1:tutor-1'
    window.sessionStorage.setItem(key, value)

    expect(loadTeachingDraft('tutor-1')).toBeUndefined()
    expect(window.sessionStorage.getItem(key)).toBeNull()
  })

  // covers: AC-14
  it('clears one completed draft or every account draft', () => {
    const first = newTeachingDraft('tutor-1', defaults)
    const second = { ...first, tutorId: 'tutor-2' }
    saveTeachingDraft(first)
    saveTeachingDraft(second)

    clearTeachingDraft('tutor-1')
    expect(window.sessionStorage.getItem('vermouth.teaching-setup.v1:tutor-1')).toBeNull()
    expect(window.sessionStorage.getItem('vermouth.teaching-setup.v1:tutor-2')).not.toBeNull()

    clearAllTeachingDrafts()
    expect(window.sessionStorage.length).toBe(0)
  })
})
