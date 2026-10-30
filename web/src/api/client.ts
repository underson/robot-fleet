import type { components, paths } from './schema'

// Names for the generated types. schema.d.ts comes from the backend's
// openapi.yaml (npm run gen:api), so a contract change breaks the build here.
type Schemas = components['schemas']
export type Site = Schemas['Site']
export type FleetOverview = Schemas['FleetOverview']
export type Summary = Schemas['Summary']
export type RobotStatus = Schemas['RobotStatus']
export type RobotDetail = Schemas['RobotDetail']
export type Health = Schemas['Health']
export type AttentionReason = Health['attention'][number]
export type HistoryPoint = Schemas['HistoryPoint']
export type FleetQuery = NonNullable<paths['/v1/fleet']['get']['parameters']['query']>

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal })
  if (!response.ok) {
    const body: { error?: string } | null = await response.json().catch(() => null)
    throw new ApiError(response.status, body?.error ?? response.statusText)
  }
  return (await response.json()) as T
}
