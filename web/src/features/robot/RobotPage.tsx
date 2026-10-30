import { Link, useParams } from 'react-router'

import { ApiError } from '../../api/client'
import { useRobot } from '../../api/queries'
import { AttentionBadges } from '../../shared/AttentionBadges'
import { connectivityLabels, formatAge, formatPercent } from '../../shared/labels'
import { BatteryChart } from './BatteryChart'

export function RobotPage() {
  const { id = '' } = useParams()
  const robot = useRobot(id)

  if (robot.isPending) return <p className="muted">Loading robot…</p>
  if (robot.isError && !robot.data) {
    const notFound = robot.error instanceof ApiError && robot.error.status === 404
    return (
      <>
        <Link to="/">← Fleet</Link>
        <p className="error">{notFound ? `No robot with id ${id}.` : `Could not load robot: ${robot.error.message}`}</p>
      </>
    )
  }

  const { name, site, model, manufacturer, serialNumber, health, state, history } = robot.data
  const now = robot.dataUpdatedAt
  return (
    <>
      <Link to="/">← Fleet</Link>
      <h2>{name}</h2>
      <p className="muted">
        {site.name} · {manufacturer} {model} · {serialNumber}
      </p>
      {robot.isError && <p className="error">Connection lost; showing the last known state.</p>}
      <AttentionBadges reasons={health.attention} />

      <dl className="facts">
        <dt>Status</dt>
        <dd className={`connectivity-${health.connectivity}`}>
          {connectivityLabels[health.connectivity]}
          {state && <span className="muted"> · last report {formatAge(state.receivedAt, now)}</span>}
        </dd>
        <dt>Battery</dt>
        <dd className={`battery-${health.battery}`}>
          {state ? formatPercent(state.power.stateOfCharge) : '—'}
          {state?.power.charging && ' (charging)'}
        </dd>
        <dt>Mode</dt>
        <dd>{state?.mode ?? '—'}</dd>
        <dt>Position</dt>
        <dd>
          {state && health.positionKnown
            ? `${state.position.x.toFixed(1)} m, ${state.position.y.toFixed(1)} m on ${state.position.mapId}`
            : 'Unknown (not localized)'}
        </dd>
        <dt>Emergency stop</dt>
        <dd>{state?.safety.emergencyStop ?? '—'}</dd>
      </dl>

      <h3>Active faults</h3>
      {state && state.faults.length > 0 ? (
        <ul>
          {state.faults.map((fault) => (
            <li key={fault.type}>
              <strong>{fault.level}</strong> {fault.type}
              {fault.description && <span className="muted"> — {fault.description}</span>}
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">None</p>
      )}

      <h3>Battery, last minutes</h3>
      <BatteryChart history={history} />
    </>
  )
}
