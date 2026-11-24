import { useState, type FormEvent } from 'react'
import { authApi, BASE } from '../api'
import { IS_APP } from '../appTarget'
import { useAuth } from '../auth/AuthContext'
import { useAuthConfig } from '../auth/useAuthConfig'
import GoogleIcon from '../components/GoogleIcon'
import { Link, navigate, Redirect, useSearch } from '../router'
import '../dashboard.css'

const ERRORS: Record<string, string> = {
  access_denied: 'Sign-in was cancelled. Try again when you are ready.',
  invalid_state: 'Your sign-in session expired. Please try again.',
  provider_failed: 'The provider could not complete sign-in. Please try again.',
}

export default function LoginPage() {
  const { user, loading, reload } = useAuth()
  const authConfig = useAuthConfig()
  const code = new URLSearchParams(useSearch()).get('error')
  const oauthError = code ? (ERRORS[code] ?? 'Sign-in failed. Please try again.') : ''

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState('')

  if (!loading && user) return <Redirect to="/dashboard" />

  const showLocal = !IS_APP && authConfig.local
  const showGoogle = IS_APP || authConfig.google
  const errorMessage = formError || oauthError

  const subtitle =
    showLocal && showGoogle
      ? 'Sign in with your email and password or Google account.'
      : showLocal
        ? 'Sign in with your email and password to access your organizations and projects.'
        : 'Use your Google account to access your organizations and projects.'

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    setFormError('')
    try {
      await authApi.localLogin(email, password)
      await reload()
      navigate('/dashboard')
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Sign-in failed. Please try again.')
      setSubmitting(false)
    }
  }

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
        <p className="login__sub">{subtitle}</p>

        {errorMessage && (
          <p className="dash-error" role="alert">
            {errorMessage}
          </p>
        )}

        {showLocal && (
          <form className="dash-form" onSubmit={(e) => void handleSubmit(e)}>
            <label className="dash-label" htmlFor="login-email">
              Email
            </label>
            <input
              id="login-email"
              className="dash-input"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={submitting}
            />
            <label className="dash-label" htmlFor="login-password">
              Password
            </label>
            <input
              id="login-password"
              className="dash-input"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={submitting}
            />
            <button
              type="submit"
              className="dash-btn dash-btn--primary"
              style={{ marginTop: 'var(--space-xs)' }}
              disabled={submitting}
            >
              {submitting ? 'Signing in…' : 'Sign in'}
            </button>
          </form>
        )}

        {showGoogle && (
          <a
            className="sso-provider-btn login__google"
            href={`${BASE}/auth/google/login`}
            onClick={
              IS_APP
                ? (e) => {
                    e.preventDefault()
                    void import('../auth/appAuth').then(({ startAppLogin }) => startAppLogin())
                  }
                : undefined
            }
          >
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
        )}

        {!IS_APP && (
          <p className="login__foot">
            <Link to="/">← Back to home</Link>
          </p>
        )}
      </div>
    </main>
  )
}
