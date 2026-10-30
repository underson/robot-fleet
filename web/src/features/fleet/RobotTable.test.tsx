import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'

import type { RobotStatus } from '../../api/client'
import { RobotTable } from './RobotTable'

const now = Date.parse('2026-10-30T10:00:10Z')

function robot(overrides: Partial<RobotStatus>): RobotStatus {
  return {
    id: 'robot-001',
    name: 'Cleaner 1',
    site: { id: 'berlin', name: 'Berlin HQ' },
    manufacturer: 'acme',
    serialNumber: 'ACME-0001',
    model: 'C1',
    health: { connectivity: 'online', battery: 'ok', positionKnown: true, attention: [] },
    state: {
      reportedAt: '2026-10-30T10:00:07Z',
      receivedAt: '2026-10-30T10:00:07Z',
      position: { mapId: 'berlin-floor-1', x: 1, y: 2, theta: 0, localized: true },
      power: { stateOfCharge: 64.4, charging: false },
      mode: 'automatic',
      faults: [],
      safety: { emergencyStop: 'none', fieldViolation: false },
    },
    ...overrides,
  }
}

function renderTable(robots: RobotStatus[]) {
  render(
    <MemoryRouter>
      <RobotTable robots={robots} now={now} />
    </MemoryRouter>,
  )
}

describe('RobotTable', () => {
  it('shows attention reasons in the order the API sent them, and links to the robot', () => {
    renderTable([
      robot({
        health: { connectivity: 'online', battery: 'critical', positionKnown: true, attention: ['emergency_stop', 'battery_critical'] },
      }),
    ])

    const row = screen.getByRole('link', { name: 'Cleaner 1' }).closest('tr')!
    expect(within(row).getAllByRole('listitem').map((badge) => badge.textContent)).toEqual([
      'Emergency stop',
      'Battery critical',
    ])
    expect(screen.getByRole('link', { name: 'Cleaner 1' })).toHaveAttribute('href', '/robots/robot-001')
    expect(within(row).getByText('3s ago')).toBeInTheDocument()
  })

  it('renders a robot that never reported without state', () => {
    renderTable([
      robot({
        id: 'robot-002',
        name: 'Cleaner 2',
        health: { connectivity: 'never_reported', battery: 'unknown', positionKnown: false, attention: [] },
        state: null,
      }),
    ])

    const row = screen.getByRole('link', { name: 'Cleaner 2' }).closest('tr')!
    expect(within(row).getByText('Never reported')).toBeInTheDocument()
    expect(within(row).queryAllByRole('listitem')).toHaveLength(0)
  })
})
