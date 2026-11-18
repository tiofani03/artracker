import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import './tokens.css'
import './base.css'
import './index.css'
import { IS_APP } from './appTarget'
import { AuthProvider } from './auth/AuthContext'
import DashboardPage from './pages/DashboardPage'
import DocsPage from './pages/DocsPage'
import LoginPage from './pages/LoginPage'
import SettingsPage from './pages/SettingsPage'
import IssuesPage from './pages/IssuesPage'
import PeoplePage from './pages/PeoplePage'
import HeatmapPage from './pages/HeatmapPage'
import IssueDetailPage from './pages/IssueDetailPage'
import InviteAcceptPage from './pages/InviteAcceptPage'
import { Redirect, useLocation } from './router'

// The landing page is web-only: in the app build this import is dead code, so the page
// never reaches the bundle.
// (import.meta.env is checked inline so the bundler can fold it; a re-exported const cannot.)
const Landing = import.meta.env.VITE_TARGET === 'app' ? null : lazy(() => import('./App.tsx'))

if (IS_APP) {
  void import('./auth/appAuth').then(({ installExternalLinkHandler }) => installExternalLinkHandler())

  // Android: tint the system-bar strips like the page, whatever the user picked in the toggle.
  const syncNativeTheme = () => {
    const bridge = (window as unknown as { ArtrackerNative?: { setTheme(t: string): void } }).ArtrackerNative
    bridge?.setTheme(document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light')
  }
  syncNativeTheme()
  new MutationObserver(syncNativeTheme).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  })
}

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
    // Temporarily hidden: Marketing Campaigns & Funnel & Retention routes
    // case '/dashboard/campaigns':
    //   return <DashboardPage section="campaigns" />
    // case '/dashboard/funnels':
    // case '/dashboard/analytics':
    //   return <DashboardPage section="funnels" />
    case '/dashboard/alerts':
      return <DashboardPage section="alerts" />
    case '/dashboard/issues':
      return <IssuesPage />
    case '/dashboard/people':
      return <PeoplePage />
    case '/dashboard/heatmap':
      return <HeatmapPage />
    case '/dashboard/settings':
      return <SettingsPage />
    default:
      if (IS_APP || !Landing) return <Redirect to="/login" />
      return (
        <Suspense fallback={null}>
          <Landing />
        </Suspense>
      )
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider>
      <Routes />
    </AuthProvider>
  </StrictMode>,
)
