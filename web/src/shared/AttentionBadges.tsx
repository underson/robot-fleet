import type { AttentionReason } from '../api/client'
import { attentionLabels } from './labels'

// Reasons arrive sorted most urgent first; the order is the server's decision.
export function AttentionBadges({ reasons }: { reasons: AttentionReason[] }) {
  if (reasons.length === 0) return null
  return (
    <ul className="badges">
      {reasons.map((reason) => (
        <li key={reason} className={`badge badge-${reason}`}>
          {attentionLabels[reason]}
        </li>
      ))}
    </ul>
  )
}
