import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './tokens.css'
import './base.css'
import './index.css'
import App from './App.tsx'
import { AuthProvider } from './auth/AuthContext'
import LoginPage from './pages/LoginPage'
import SettingsPage from './pages/SettingsPage'
import { Redirect, useLocation } from './router'

function Routes() {
  const path = useLocation()
  switch (path.replace(/\/+$/, '') || '/') {
    case '/login':
      return <LoginPage />
    case '/dashboard':
      return <Redirect to="/dashboard/settings" />
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
