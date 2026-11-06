import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './tokens.css'
import './base.css'
import './index.css'
import App from './App.tsx'
import { AuthProvider } from './auth/AuthContext'
import DashboardPage from './pages/DashboardPage'
import DocsPage from './pages/DocsPage'
import LoginPage from './pages/LoginPage'
import SettingsPage from './pages/SettingsPage'
import IssuesPage from './pages/IssuesPage'
import IssueDetailPage from './pages/IssueDetailPage'
import InviteAcceptPage from './pages/InviteAcceptPage'
import { useLocation } from './router'

function Routes() {
  const path = useLocation().replace(/\/+$/, '') || '/'
  if (path.startsWith('/invite/')) return <InviteAcceptPage />
  if (path.startsWith('/dashboard/issues/')) return <IssueDetailPage />
  switch (path) {
    case '/login':
      return <LoginPage />
    case '/docs':
    case '/dashboard/docs':
      return <DocsPage />
    case '/dashboard':
      return <DashboardPage section="overview" />
    case '/dashboard/replays':
      return <DashboardPage section="replays" />
    case '/dashboard/logs':
      return <DashboardPage section="logs" />
    case '/dashboard/campaigns':
      return <DashboardPage section="campaigns" />
    case '/dashboard/funnels':
    case '/dashboard/analytics':
      return <DashboardPage section="funnels" />
    case '/dashboard/alerts':
      return <DashboardPage section="alerts" />
    case '/dashboard/issues':
      return <IssuesPage />
    case '/dashboard/settings':
      return <SettingsPage />
    default:
      return <App />
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider>
      <Routes />
    </AuthProvider>
  </StrictMode>,
)
