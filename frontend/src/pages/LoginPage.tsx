import { BASE } from '../api'
import { useAuth } from '../auth/AuthContext'
import { Link, Redirect } from '../router'

const ERRORS: Record<string, string> = {
  access_denied: 'Sign-in was cancelled. Try again when you are ready.',
  invalid_state: 'Your sign-in session expired. Please try again.',
  provider_failed: 'The provider could not complete sign-in. Please try again.',
}

export default function LoginPage() {
  const { user, loading } = useAuth()
  const code = new URLSearchParams(window.location.search).get('error')
  const errorMessage = code ? (ERRORS[code] ?? 'Sign-in failed. Please try again.') : ''
  if (!loading && user) return <Redirect to="/dashboard" />

  return (
    <main className="auth-page">
      <div className="auth-card">
        <Link to="/" className="brand auth-brand">
          <div className="brand-icon">A</div>
          <span>ARTracker</span>
        </Link>
        <h1 className="auth-title">Sign in</h1>
        <p className="auth-desc">Use your organization identity to access the dashboard.</p>

        {errorMessage && (
          <p className="form-error" role="alert">
            {errorMessage}
          </p>
        )}

        <div className="sso-provider-list">
          <a className="sso-btn" href={`${BASE}/auth/google/login`}>
            <svg width="18" height="18" viewBox="0 0 24 24"><path fill="#EA4335" d="M12 5c1.6 0 3 .6 4.1 1.6l3.1-3.1C17.3 1.8 14.8 1 12 1 7.5 1 3.7 3.6 1.9 7.3l3.7 2.9C6.5 7.4 9 5 12 5z"/><path fill="#4285F4" d="M23.5 12.3c0-.8-.1-1.6-.2-2.3H12v4.5h6.5c-.3 1.5-1.1 2.8-2.4 3.7l3.7 2.9c2.2-2 3.7-5 3.7-8.8z"/><path fill="#FBBC05" d="M5.6 14.8c-.2-.7-.4-1.5-.4-2.3s.2-1.6.4-2.3L1.9 7.3C.7 9.7 0 12 0 14.8s.7 5.1 1.9 7.5l3.7-2.9z"/><path fill="#34A853" d="M12 23.5c3.2 0 6-1.1 8-3l-3.7-2.9c-1.1.7-2.5 1.2-4.3 1.2-3 0-5.5-2.4-6.4-5.2L1.9 16.5C3.7 20.2 7.5 23.5 12 23.5z"/></svg>
            <span>Continue with Google</span>
          </a>
        </div>
      </div>
    </main>
  )
}
