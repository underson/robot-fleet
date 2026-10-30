import { useFleet, useSites } from '../../api/queries'
import { useFleetFilters, type StatusFilter } from './filters'
import { RobotTable } from './RobotTable'
import { SummaryTiles } from './SummaryTiles'

const statusTabs: { value?: StatusFilter; label: string }[] = [
  { label: 'All' },
  { value: 'attention', label: 'Needs attention' },
  { value: 'online', label: 'Online' },
  { value: 'offline', label: 'Offline' },
]

export function FleetPage() {
  const [filters, setFilters] = useFleetFilters()
  const sites = useSites()
  const fleet = useFleet(filters)

  return (
    <>
      <div className="toolbar">
        <label>
          Site{' '}
          <select value={filters.site ?? ''} onChange={(event) => setFilters({ ...filters, site: event.target.value })}>
            <option value="">All sites</option>
            {sites.data?.map((site) => (
              <option key={site.id} value={site.id}>
                {site.name}
              </option>
            ))}
          </select>
        </label>
        <nav className="tabs" aria-label="Status filter">
          {statusTabs.map((tab) => (
            <button
              key={tab.label}
              type="button"
              aria-pressed={filters.status === tab.value}
              onClick={() => setFilters({ ...filters, status: tab.value })}
            >
              {tab.label}
            </button>
          ))}
        </nav>
      </div>

      {fleet.isPending && <p className="muted">Loading fleet…</p>}
      {fleet.isError && !fleet.data && <p className="error">Could not load the fleet: {fleet.error.message}</p>}
      {fleet.data && (
        <>
          {fleet.isError && <p className="error">Connection lost; showing the last known state.</p>}
          <SummaryTiles summary={fleet.data.summary} />
          {fleet.data.robots.length === 0 ? (
            <p className="muted">No robots match this filter.</p>
          ) : (
            <RobotTable robots={fleet.data.robots} now={fleet.dataUpdatedAt} />
          )}
        </>
      )}
    </>
  )
}
