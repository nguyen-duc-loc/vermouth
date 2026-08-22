import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { refreshSession } from './api/session'
import { router } from './routes'
import './styles.css'

// TanStack Query's cache is where eventual consistency is made honest on
// screen: a projection that has not caught up yet is refetched rather than
// guessed at (spec 0002, web app).
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 0 } },
})

const root = document.getElementById('root')
if (!root) throw new Error('index.html is missing the root element')

// The boot time refresh is what makes a reload survive without a Google round
// trip: the cookie is the only thing that persists, and this is the one call that
// turns it into an access token (spec 0004, AC-3). It runs before the first
// render so no screen flashes the wrong state.
await refreshSession()

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
