import { describe, expect, it } from 'vitest'

import { parseFleetFilters } from './filters'

describe('parseFleetFilters', () => {
  it('reads site and status from the URL', () => {
    expect(parseFleetFilters(new URLSearchParams('site=berlin&status=attention'))).toEqual({
      site: 'berlin',
      status: 'attention',
    })
  })

  it('drops an unknown status instead of sending it to the API', () => {
    expect(parseFleetFilters(new URLSearchParams('status=sleeping'))).toEqual({ site: undefined, status: undefined })
  })
})
