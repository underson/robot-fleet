import { Link } from 'react-router'

import type { RobotStatus } from '../../api/client'
import { AttentionBadges } from '../../shared/AttentionBadges'
import { connectivityLabels, formatAge, formatPercent } from '../../shared/labels'

// Rows come sorted by the API (most urgent first), so the table never re-sorts:
// urgency is a domain rule and lives in one place.
export function RobotTable({ robots, now }: { robots: RobotStatus[]; now: number }) {
  return (
    <table className="robots">
      <thead>
        <tr>
          <th>Robot</th>
          <th>Site</th>
          <th>Status</th>
          <th>Battery</th>
          <th>Mode</th>
          <th>Needs attention</th>
          <th>Last report</th>
        </tr>
      </thead>
      <tbody>
        {robots.map((robot) => (
          <tr key={robot.id} className={robot.health.attention.length > 0 ? 'row-attention' : undefined}>
            <td>
              <Link to={`/robots/${robot.id}`}>{robot.name}</Link>
              <div className="muted">{robot.serialNumber}</div>
            </td>
            <td>{robot.site.name}</td>
            <td className={`connectivity-${robot.health.connectivity}`}>
              {connectivityLabels[robot.health.connectivity]}
            </td>
            <td className={`battery-${robot.health.battery}`}>
              {robot.state ? formatPercent(robot.state.power.stateOfCharge) : '—'}
              {robot.state?.power.charging && <span className="muted"> charging</span>}
            </td>
            <td>{robot.state?.mode ?? '—'}</td>
            <td>
              <AttentionBadges reasons={robot.health.attention} />
            </td>
            <td>{robot.state ? formatAge(robot.state.receivedAt, now) : '—'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
