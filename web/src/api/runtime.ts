/** Public settings mounted beside the static web files at runtime. */
export type RuntimeConfig = {
  googleAuthEnabled: boolean
}

let current: RuntimeConfig = { googleAuthEnabled: false }

/** Loads runtime settings without letting a missing or malformed file enable auth. */
export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  try {
    const response = await fetch('/config.json', { cache: 'no-store' })
    if (!response.ok) return current
    const value: unknown = await response.json()
    if (
      typeof value === 'object' &&
      value !== null &&
      'googleAuthEnabled' in value &&
      typeof value.googleAuthEnabled === 'boolean'
    ) {
      current = { googleAuthEnabled: value.googleAuthEnabled }
    }
  } catch {
    return current
  }
  return current
}

/** Returns the settings loaded before the React tree was mounted. */
export function runtimeConfig(): RuntimeConfig {
  return current
}
