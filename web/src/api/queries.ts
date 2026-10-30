import { keepPreviousData, useQuery } from '@tanstack/react-query'

import { getJSON, type FleetOverview, type FleetQuery, type RobotDetail, type Site } from './client'

// The API derives health from the server clock, so polling is all the UI needs
// to stay current. Robots report every second; 2 s is fresh enough for a person.
// TanStack Query pauses polling while the tab is hidden.
const POLL_INTERVAL_MS = 2_000

// The console shows the whole fleet on one page. The API pages (max 200);
// a paged table is listed under "with more time" in the README.
const FLEET_PAGE_SIZE = 200

export function useSites() {
  return useQuery({
    queryKey: ['sites'],
    queryFn: ({ signal }) => getJSON<Site[]>('/v1/sites', signal),
    staleTime: Infinity, // sites change with deployments, not at runtime
  })
}

export function useFleet(filters: Pick<FleetQuery, 'site' | 'status'>) {
  return useQuery({
    queryKey: ['fleet', filters],
    queryFn: ({ signal }) => {
      const params = new URLSearchParams({ limit: String(FLEET_PAGE_SIZE) })
      if (filters.site) params.set('site', filters.site)
      if (filters.status) params.set('status', filters.status)
      return getJSON<FleetOverview>(`/v1/fleet?${params}`, signal)
    },
    refetchInterval: POLL_INTERVAL_MS,
    placeholderData: keepPreviousData, // keep the table while a new filter loads
  })
}

export function useRobot(id: string) {
  return useQuery({
    queryKey: ['robot', id],
    queryFn: ({ signal }) => getJSON<RobotDetail>(`/v1/robots/${encodeURIComponent(id)}`, signal),
    refetchInterval: POLL_INTERVAL_MS,
  })
}
