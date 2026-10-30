import type { Summary } from '../../api/client'

export function SummaryTiles({ summary }: { summary: Summary }) {
  const tiles = [
    { label: 'Need attention', value: summary.needsAttention, alert: summary.needsAttention > 0 },
    { label: 'Online', value: summary.online, alert: false },
    { label: 'Offline', value: summary.offline, alert: summary.offline > 0 },
    { label: 'Low battery', value: summary.lowBattery, alert: false },
    { label: 'Never reported', value: summary.neverReported, alert: false },
  ]
  return (
    <dl className="tiles">
      {tiles.map((tile) => (
        <div key={tile.label} className={tile.alert ? 'tile tile-alert' : 'tile'}>
          <dt>{tile.label}</dt>
          <dd>{tile.value}</dd>
        </div>
      ))}
    </dl>
  )
}
