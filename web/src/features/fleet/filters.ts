import { useSearchParams } from 'react-router'

import type { FleetQuery } from '../../api/client'

export type StatusFilter = NonNullable<FleetQuery['status']>
export const statusFilters: readonly StatusFilter[] = ['attention', 'online', 'offline']

export interface FleetFilters {
  site?: string
  status?: StatusFilter
}

// Filters live in the URL, so a view can be bookmarked, shared and survives a
// reload. Unknown values are dropped rather than sent to the API.
export function parseFleetFilters(params: URLSearchParams): FleetFilters {
  const status = statusFilters.find((candidate) => candidate === params.get('status'))
  return { site: params.get('site') || undefined, status }
}

export function useFleetFilters() {
  const [searchParams, setSearchParams] = useSearchParams()

  const setFilters = (next: FleetFilters) => {
    const params = new URLSearchParams()
    if (next.site) params.set('site', next.site)
    if (next.status) params.set('status', next.status)
    setSearchParams(params, { replace: true })
  }

  return [parseFleetFilters(searchParams), setFilters] as const
}
