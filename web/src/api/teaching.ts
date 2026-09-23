import { api, authHeaders } from './client'
import type { components } from './schema'
import { withProtectedRetry } from './session'

export type ApiError = components['schemas']['Error']
export type AttendanceSave = components['schemas']['AttendanceSave']
export type AttendanceSheet = components['schemas']['AttendanceSheet']
export type AttendanceState = components['schemas']['AttendanceState']
export type BillingProjection = components['schemas']['BillingProjection']
export type ClassColor = components['schemas']['ClassColor']
export type ClassRoster = components['schemas']['ClassRoster']
export type ChangeRosterInput = components['schemas']['ChangeRosterRequest']
export type CreateClassInput = components['schemas']['CreateClassRequest']
export type CreateClassResult = components['schemas']['CreateClassResponse']
export type CreateStudentInput = components['schemas']['CreateStudentRequest']
export type EndScheduleInput = components['schemas']['EndScheduleRequest']
export type EndScheduleResult = components['schemas']['EndScheduleResponse']
export type Home = components['schemas']['Home']
export type HomeBillingProjection = components['schemas']['HomeBillingProjection']
export type HomeSession = components['schemas']['HomeSession']
export type MoveSessionInput = components['schemas']['MoveSessionRequest']
export type CanonicalSession = components['schemas']['CanonicalSession']
export type PutScheduleInput = components['schemas']['PutScheduleRequest']
export type PutScheduleResult = components['schemas']['PutScheduleResponse']
export type RosterStudent = components['schemas']['RosterStudent']
export type SetupDefaults = components['schemas']['SetupDefaults']
export type SaveAttendanceInput = components['schemas']['SaveAttendanceRequest']
export type Schedule = components['schemas']['Schedule']
export type ScheduleSession = components['schemas']['ScheduleSession']
export type SessionVersionInput = components['schemas']['SessionVersionRequest']
export type Student = components['schemas']['StudentRecord']
export type StudentDetail = components['schemas']['StudentDetail']
export type StudentMembership = components['schemas']['StudentMembership']
export type StudentPage = components['schemas']['StudentPage']
export type StudentSummary = components['schemas']['StudentSummary']
export type UpdateStudentInput = components['schemas']['UpdateStudentRequest']
export type Tutor = components['schemas']['Tutor']
export type ClassReference = components['schemas']['ClassReference']

export const classRosterKeys = {
  all: ['class-rosters'] as const,
  detail: (tutorId: string, classId: string, date?: string) =>
    [...classRosterKeys.all, tutorId, classId, date ?? null] as const,
}

export const attendanceKeys = {
  all: ['attendance'] as const,
  detail: (tutorId: string, sessionId: string) =>
    [...attendanceKeys.all, tutorId, sessionId] as const,
}

export const studentKeys = {
  all: ['students'] as const,
  lists: () => [...studentKeys.all, 'list'] as const,
  list: (tutorId: string, query: string, cursor?: string) =>
    [...studentKeys.lists(), tutorId, query, cursor ?? null] as const,
  details: () => [...studentKeys.all, 'detail'] as const,
  detail: (tutorId: string, studentId: string) =>
    [...studentKeys.details(), tutorId, studentId] as const,
}

function errorMessage(error: unknown, fallback: string) {
  const shaped = error as ApiError | undefined
  return shaped?.error?.message ?? fallback
}

export class TeachingApiError extends Error {
  readonly body: ApiError

  constructor(body: ApiError, fallback: string) {
    super(errorMessage(body, fallback))
    this.name = 'TeachingApiError'
    this.body = body
  }
}

function throwTeachingError(error: unknown, fallback: string): never {
  if (error && typeof error === 'object' && 'error' in error) {
    throw new TeachingApiError(error as ApiError, fallback)
  }
  throw new Error(fallback)
}

export async function readHome(cursor: string | undefined, signal?: AbortSignal): Promise<Home> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/home', {
      headers: authHeaders(),
      params: { query: { cursor } },
      signal,
    }),
  )
  if (error || !data) throw new Error(errorMessage(error, 'the gateway could not read home'))
  return data
}

export async function readBillingProjection(signal?: AbortSignal): Promise<HomeBillingProjection> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/home/billing-projection', { headers: authHeaders(), signal }),
  )
  if (error || !data) {
    throw new Error(errorMessage(error, 'the gateway could not read billing progress'))
  }
  return data
}

export async function readTutor(signal?: AbortSignal): Promise<Tutor> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/me', { headers: authHeaders(), signal }),
  )
  if (error || !data) throw new Error(errorMessage(error, 'the gateway could not read the tutor'))
  return data
}

export async function readSchedule(
  input: {
    from: string
    through: string
    classIds: string[]
    includeReplaced?: boolean
    historyLimit?: number
    historyCursor?: string
  },
  signal?: AbortSignal,
): Promise<Schedule> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/schedule', {
      headers: authHeaders(),
      params: {
        query: {
          from: input.from,
          through: input.through,
          class_id: input.classIds.length > 0 ? input.classIds : undefined,
          include_replaced: input.includeReplaced,
          history_limit: input.historyLimit,
          history_cursor: input.historyCursor,
        },
      },
      signal,
    }),
  )
  if (error || !data)
    throw new Error(errorMessage(error, 'the gateway could not read the schedule'))
  return data
}

export async function putSchedule(
  classId: string,
  input: PutScheduleInput,
  idempotencyKey: string,
): Promise<PutScheduleResult> {
  const { data, error } = await api.PUT('/api/classes/{class_id}/schedule', {
    headers: authHeaders(),
    params: {
      path: { class_id: classId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the schedule could not be saved')
  return data
}

export async function endSchedule(
  classId: string,
  input: EndScheduleInput,
  idempotencyKey: string,
): Promise<EndScheduleResult> {
  const { data, error } = await api.POST('/api/classes/{class_id}/schedule/end', {
    headers: authHeaders(),
    params: {
      path: { class_id: classId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the schedule could not be ended')
  return data
}

export async function moveSession(
  sessionId: string,
  input: MoveSessionInput,
  idempotencyKey: string,
): Promise<CanonicalSession> {
  const { data, error } = await api.POST('/api/sessions/{session_id}/move', {
    headers: authHeaders(),
    params: {
      path: { session_id: sessionId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the session could not be moved')
  return data
}

async function changeSessionState(
  path: '/api/sessions/{session_id}/cancel' | '/api/sessions/{session_id}/restore',
  sessionId: string,
  input: SessionVersionInput,
  idempotencyKey: string,
): Promise<CanonicalSession> {
  const { data, error } = await api.POST(path, {
    headers: authHeaders(),
    params: {
      path: { session_id: sessionId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the session state could not be changed')
  return data
}

export function cancelSession(
  sessionId: string,
  input: SessionVersionInput,
  idempotencyKey: string,
) {
  return changeSessionState('/api/sessions/{session_id}/cancel', sessionId, input, idempotencyKey)
}

export function restoreSession(
  sessionId: string,
  input: SessionVersionInput,
  idempotencyKey: string,
) {
  return changeSessionState('/api/sessions/{session_id}/restore', sessionId, input, idempotencyKey)
}

export async function createClass(
  input: CreateClassInput,
  idempotencyKey: string,
): Promise<CreateClassResult> {
  const { data, error } = await api.POST('/api/classes', {
    headers: authHeaders(),
    params: { header: { 'Idempotency-Key': idempotencyKey } },
    body: input,
  })
  if (error || !data) throw new Error(errorMessage(error, 'the class could not be created'))
  return data
}

export async function createStudent(
  input: CreateStudentInput,
  idempotencyKey: string,
): Promise<Student> {
  const { data, error } = await api.POST('/api/students', {
    headers: authHeaders(),
    params: { header: { 'Idempotency-Key': idempotencyKey } },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the student could not be created')
  return data
}

export async function readStudents(
  query: string,
  cursor: string | undefined,
  signal?: AbortSignal,
): Promise<StudentPage> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/students', {
      headers: authHeaders(),
      params: { query: { q: query || undefined, cursor } },
      signal,
    }),
  )
  if (error || !data) throwTeachingError(error, 'the student list could not be read')
  return data
}

export async function readStudent(studentId: string, signal?: AbortSignal): Promise<StudentDetail> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/students/{student_id}', {
      headers: authHeaders(),
      params: { path: { student_id: studentId } },
      signal,
    }),
  )
  if (error || !data) throwTeachingError(error, 'the student could not be read')
  return data
}

export async function updateStudent(
  studentId: string,
  input: UpdateStudentInput,
  idempotencyKey: string,
): Promise<Student> {
  const { data, error } = await api.PATCH('/api/students/{student_id}', {
    headers: authHeaders(),
    params: {
      path: { student_id: studentId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the student could not be updated')
  return data
}

export async function archiveStudent(studentId: string, idempotencyKey: string): Promise<void> {
  const { error } = await api.DELETE('/api/students/{student_id}', {
    headers: authHeaders(),
    params: {
      path: { student_id: studentId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
  })
  if (error) throwTeachingError(error, 'the student could not be archived')
}

export async function readClassRoster(
  classId: string,
  date: string | undefined,
  signal?: AbortSignal,
): Promise<ClassRoster> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/classes/{class_id}/roster', {
      headers: authHeaders(),
      params: { path: { class_id: classId }, query: { date } },
      signal,
    }),
  )
  if (error || !data) throwTeachingError(error, 'the class roster could not be read')
  return data
}

export async function changeClassRoster(
  classId: string,
  input: ChangeRosterInput,
  idempotencyKey: string,
): Promise<ClassRoster> {
  const { data, error } = await api.PUT('/api/classes/{class_id}/roster', {
    headers: authHeaders(),
    params: {
      path: { class_id: classId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'the class roster could not be changed')
  return data
}

export async function readAttendance(
  sessionId: string,
  signal?: AbortSignal,
): Promise<AttendanceSheet> {
  const { data, error } = await withProtectedRetry(() =>
    api.GET('/api/sessions/{session_id}/attendance', {
      headers: authHeaders(),
      params: { path: { session_id: sessionId } },
      signal,
    }),
  )
  if (error || !data) throwTeachingError(error, 'attendance could not be read')
  return data
}

export async function saveAttendance(
  sessionId: string,
  input: SaveAttendanceInput,
  idempotencyKey: string,
): Promise<AttendanceSave> {
  const { data, error } = await api.PUT('/api/sessions/{session_id}/attendance', {
    headers: authHeaders(),
    params: {
      path: { session_id: sessionId },
      header: { 'Idempotency-Key': idempotencyKey },
    },
    body: input,
  })
  if (error || !data) throwTeachingError(error, 'attendance could not be saved')
  return data
}
