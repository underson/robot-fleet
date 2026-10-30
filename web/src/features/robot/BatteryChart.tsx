import type { HistoryPoint } from '../../api/client'

const WIDTH = 600
const HEIGHT = 120

// A plain SVG line: one series over a few minutes doesn't need a chart library.
export function BatteryChart({ history }: { history: HistoryPoint[] }) {
  const first = history[0]
  const last = history[history.length - 1]
  if (!first || !last || first === last) {
    return <p className="muted">Not enough history to draw battery over time.</p>
  }

  const start = Date.parse(first.reportedAt)
  const span = Date.parse(last.reportedAt) - start
  const points = history
    .map((point) => {
      const x = ((Date.parse(point.reportedAt) - start) / span) * WIDTH
      const y = HEIGHT - (point.power.stateOfCharge / 100) * HEIGHT
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')

  return (
    <figure className="chart">
      <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} preserveAspectRatio="none" role="img" aria-label="Battery over time">
        <polyline points={points} fill="none" stroke="currentColor" strokeWidth="2" vectorEffect="non-scaling-stroke" />
      </svg>
      <figcaption className="muted">
        Battery, 0–100%, {new Date(start).toLocaleTimeString()} to {new Date(Date.parse(last.reportedAt)).toLocaleTimeString()}
      </figcaption>
    </figure>
  )
}
