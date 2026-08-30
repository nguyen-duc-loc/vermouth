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
export type Home = components['schemas']['Home']
export type HomeBillingProjection = components['schemas']['HomeBillingProjection']
export type HomeSession = components['schemas']['HomeSession']
export type JoinRosterInput = components['schemas']['JoinRosterRequest']
export type MarkAttendanceInput = components['schemas']['MarkAttendanceRequest']
export type RosterPeriod = components['schemas']['RosterPeriod']
export type SetupDefaults = components['schemas']['SetupDefaults']
export type Student = components['schemas']['Student']

function errorMessage(error: unknown, fallback: string) {
  const shaped = error as ApiError | undefined
  return shaped?.error?.message ?? fallback
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
