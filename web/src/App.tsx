import './styles.css'

import { AdminDashboard } from './features/admin/AdminDashboard'
import { AuthGate } from './features/auth/AuthGate'
import { PublicStatusPage } from './features/public/PublicStatusPage'

export function App() {
  const path = typeof window === 'undefined' ? '/' : window.location.pathname
  if (path === '/public' || path.startsWith('/public/')) {
    return <PublicStatusPage />
  }
  return (
    <AuthGate>
      {(session, refreshSession, logout, onUnauthorized) => (
        <AdminDashboard session={session} refreshSession={refreshSession} logout={logout} onUnauthorized={onUnauthorized} />
      )}
    </AuthGate>
  )
}
