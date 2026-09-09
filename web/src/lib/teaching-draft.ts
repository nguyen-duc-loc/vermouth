import type { ClassColor, SetupDefaults } from '../api/teaching'

const draftPrefix = 'vermouth.teaching-setup.v1:'

export type TeachingDraftStep = 'class' | 'student' | 'roster'

export type TeachingDraftValues = {
  className: string
  rateAmount: string
  color: 'suggest' | ClassColor
  localDate: string
  startTime: string
  endTime: string
  studentName: string
  phone: string
}

export type TeachingDraft = {
  tutorId: string
  step: TeachingDraftStep
  classKey: string
  studentKey: string
  classId?: string
  sessionId?: string
  studentId?: string
  firstLocalDate?: string
  values: TeachingDraftValues
}

function draftKey(tutorId: string) {
  return `${draftPrefix}${tutorId}`
}

function storageAvailable() {
  return typeof window !== 'undefined' && typeof window.sessionStorage !== 'undefined'
}

function clearStoredTeachingDrafts(exceptKey?: string) {
  if (!storageAvailable()) return
  for (let index = window.sessionStorage.length - 1; index >= 0; index -= 1) {
    const key = window.sessionStorage.key(index)
    if (key?.startsWith(draftPrefix) && key !== exceptKey) {
      window.sessionStorage.removeItem(key)
    }
  }
}

export function newTeachingDraft(tutorId: string, defaults: SetupDefaults): TeachingDraft {
  return {
    tutorId,
    step: 'class',
    classKey: crypto.randomUUID(),
    studentKey: crypto.randomUUID(),
    values: {
      className: '',
      rateAmount: '',
      color: 'suggest',
      localDate: defaults.local_date,
      startTime: defaults.start_time,
      endTime: defaults.end_time,
      studentName: '',
      phone: '',
    },
  }
}

export function loadTeachingDraft(tutorId: string): TeachingDraft | undefined {
  if (!storageAvailable()) return undefined
  clearStoredTeachingDrafts(draftKey(tutorId))
  const raw = window.sessionStorage.getItem(draftKey(tutorId))
  if (!raw) return undefined
  try {
    const parsed: unknown = JSON.parse(raw)
    if (
      typeof parsed === 'object' &&
      parsed !== null &&
      'tutorId' in parsed &&
      parsed.tutorId === tutorId &&
      'step' in parsed &&
      (parsed.step === 'class' || parsed.step === 'student' || parsed.step === 'roster') &&
      'values' in parsed
    ) {
      return parsed as TeachingDraft
    }
  } catch {
    window.sessionStorage.removeItem(draftKey(tutorId))
    return undefined
  }
  window.sessionStorage.removeItem(draftKey(tutorId))
  return undefined
}

export function saveTeachingDraft(draft: TeachingDraft) {
  if (!storageAvailable()) return
  window.sessionStorage.setItem(draftKey(draft.tutorId), JSON.stringify(draft))
}

export function clearTeachingDraft(tutorId: string) {
  if (!storageAvailable()) return
  window.sessionStorage.removeItem(draftKey(tutorId))
}

/** Clears contact data and command keys before this tab leaves an account context. */
export function clearAllTeachingDrafts() {
  clearStoredTeachingDrafts()
}
