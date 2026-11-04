import { BASE } from '../api'
import { useAuth } from '../auth/AuthContext'
import GoogleIcon from '../components/GoogleIcon'
import { Link, Redirect } from '../router'
import '../dashboard.css'

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
    <main className="login">
      <div className="login__card">
        <div className="sso-brand login__brand">
          <span className="sso-logo-badge" aria-hidden="true">
            <svg className="sso-logo-badge__svg" viewBox="0 0 24 24" fill="none">
              <path d="M4 19L12 4L20 19M7.5 14H16.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="square" />
            </svg>
          </span>
          <span className="sso-brand__meta">
            <span className="sso-brand__name">ARTracker</span>
            <span className="sso-brand__tag">Telemetry Console</span>
          </span>
        </div>

        <h1 className="login__title">Sign in to ARTracker</h1>
        <p className="login__sub">Use your Google account to access your organizations and projects.</p>

        {errorMessage && (
          <p className="dash-error" role="alert">
            {errorMessage}
          </p>
        )}

        <a className="sso-provider-btn login__google" href={`${BASE}/auth/google/login`}>
          <span className="sso-provider-btn__left">
            <span className="sso-provider-btn__icon" aria-hidden="true">
              <GoogleIcon size={16} />
            </span>
            <span>Continue with Google</span>
          </span>
          <span className="sso-provider-btn__right">
            <svg className="sso-provider-btn__arrow" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path d="M5 12h14m-5-5 5 5-5 5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </span>
        </a>

        <p className="login__foot">
          <Link to="/">← Back to home</Link>
        </p>
      </div>
    </main>
  )
}
