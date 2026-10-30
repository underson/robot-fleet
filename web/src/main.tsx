import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'

import { ApiError } from './api/client'
import { FleetPage } from './features/fleet/FleetPage'
import { RobotPage } from './features/robot/RobotPage'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx won't fix itself on retry; network errors and 5xx might.
      retry: (failureCount, error) => !(error instanceof ApiError && error.status < 500) && failureCount < 2,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <header className="header">
          <h1>Fleet console</h1>
        </header>
        <main>
          <Routes>
            <Route path="/" element={<FleetPage />} />
            <Route path="/robots/:id" element={<RobotPage />} />
          </Routes>
        </main>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
