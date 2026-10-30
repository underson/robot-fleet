import type { AttentionReason, Health } from '../api/client'

// Record<Union, string> makes the compiler flag a reason the API adds but the UI
// has no label for.
export const attentionLabels: Record<AttentionReason, string> = {
  emergency_stop: 'Emergency stop',
  severe_fault: 'Severe fault',
  offline: 'Offline',
  battery_critical: 'Battery critical',
}

export const connectivityLabels: Record<Health['connectivity'], string> = {
  online: 'Online',
  offline: 'Offline',
  never_reported: 'Never reported',
}

export function formatAge(timestamp: string, now: number): string {
  const seconds = Math.max(0, Math.round((now - Date.parse(timestamp)) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  return `${Math.floor(seconds / 3600)}h ago`
}

export function formatPercent(value: number): string {
  return `${Math.round(value)}%`
}
