import { api, authHeaders } from './client'
import type { components } from './schema'
import { withProtectedRetry } from './session'

export type ApiError = components['schemas']['Error']
export type Attendance = components['schemas']['Attendance']
export type AttendanceState = components['schemas']['AttendanceState']
export type BillingProjection = components['schemas']['BillingProjection']
export type ClassColor = components['schemas']['ClassColor']
export type CreateClassInput = components['schemas']['CreateClassRequest']
export type CreateClassResult = components['schemas']['CreateClassResponse']
export type CreateStudentInput = components['schemas']['CreateStudentRequest']
export type EndScheduleInput = components['schemas']['EndScheduleRequest']
export type EndScheduleResult = components['schemas']['EndScheduleResponse']
export type Home = components['schemas']['Home']
export type HomeBillingProjection = components['schemas']['HomeBillingProjection']
export type HomeSession = components['schemas']['HomeSession']
export type JoinRosterInput = components['schemas']['JoinRosterRequest']
export type MarkAttendanceInput = components['schemas']['MarkAttendanceRequest']
export type MoveSessionInput = components['schemas']['MoveSessionRequest']
export type CanonicalSession = components['schemas']['CanonicalSession']
export type PutScheduleInput = components['schemas']['PutScheduleRequest']
export type PutScheduleResult = components['schemas']['PutScheduleResponse']
export type RosterPeriod = components['schemas']['RosterPeriod']
export type SetupDefaults = components['schemas']['SetupDefaults']
export type Schedule = components['schemas']['Schedule']
export type ScheduleSession = components['schemas']['ScheduleSession']
export type SessionVersionInput = components['schemas']['SessionVersionRequest']
export type Student = components['schemas']['Student']
export type Tutor = components['schemas']['Tutor']

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
  if (error || !data) throw new Error(errorMessage(error, 'the student could not be created'))
  return data
}

export async function joinRoster(classId: string, input: JoinRosterInput): Promise<RosterPeriod> {
  const { data, error } = await api.POST('/api/classes/{class_id}/roster', {
    headers: authHeaders(),
    params: { path: { class_id: classId } },
    body: input,
  })
  if (error || !data) throw new Error(errorMessage(error, 'the student could not join the class'))
  return data
}

export async function markAttendance(
  sessionId: string,
  studentId: string,
  input: MarkAttendanceInput,
): Promise<Attendance> {
  const { data, error } = await api.PUT('/api/sessions/{session_id}/attendance/{student_id}', {
    headers: authHeaders(),
    params: { path: { session_id: sessionId, student_id: studentId } },
    body: input,
  })
  if (error || !data) throw new Error(errorMessage(error, 'attendance could not be saved'))
  return data
}
