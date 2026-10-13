import './styles.css'

import { AdminDashboard } from './features/admin/AdminDashboard'
import { AuthGate } from './features/auth/AuthGate'
import { PublicStatusPage } from './features/public/PublicStatusPage'
import { I18nProvider } from './i18n'
import { ThemeProvider } from './theme'

export function App() {
  return <ThemeProvider><I18nProvider><AppRoutes /></I18nProvider></ThemeProvider>
}

function AppRoutes() {
  const path = typeof window === 'undefined' ? '/' : window.location.pathname
  if (path !== '/admin' && !path.startsWith('/admin/')) {
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
