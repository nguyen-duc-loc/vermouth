import createClient from 'openapi-fetch'
import type { paths } from './schema'

// The browser's types come from api/openapi.yaml through openapi-typescript,
// the same document the gateway generates its Go types from (STK-10). A hand
// written client type for a gateway endpoint is not allowed.
//
// The base URL is empty by default, so requests go to this origin and through
// the Vite proxy in development, which is what keeps the gateway free of CORS.
// VITE_API_BASE_URL can point somewhere else, and whoever sets it owns adding
// CORS at the gateway too.
const baseUrl = import.meta.env.VITE_API_BASE_URL ?? ''

export const api = createClient<paths>({ baseUrl })

// The access token lives in memory only, so closing the tab forgets it.
// Feature 7 owns token lifetime, refresh rotation and where the token is kept.
let accessToken: string | null = null

export function setAccessToken(token: string | null) {
  accessToken = token
}

export function authHeaders(): Record<string, string> {
  return accessToken ? { Authorization: `Bearer ${accessToken}` } : {}
}

export function hasAccessToken() {
  return accessToken !== null
}
