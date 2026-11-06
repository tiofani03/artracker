import { useEffect, useState } from 'react'
import { BASE, teamApi, type InvitePreview } from '../api'
import { useAuth } from '../auth/AuthContext'
import GoogleIcon from '../components/GoogleIcon'
import { Link, navigate, useLocation } from '../router'
import '../dashboard.css'

export default function InviteAcceptPage() {
  const path = useLocation()
  const token = path.replace(/^\/invite\/?/, '').split('/')[0] || ''

  const { user, loading: authLoading } = useAuth()
  const [preview, setPreview] = useState<InvitePreview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [accepting, setAccepting] = useState(false)
  const [success, setSuccess] = useState(false)

  useEffect(() => {
    if (!token) {
      setError('Invalid invitation link.')
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)

    teamApi
      .getInvitePreview(token)
      .then((res) => {
        if (cancelled) return
        setPreview(res)
        setLoading(false)
      })
      .catch((err) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Invitation not found or expired')
        setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [token])

  const handleAccept = async () => {
    if (!token) return
    setAccepting(true)
    setError(null)
    try {
      await teamApi.acceptInvite(token)
      setSuccess(true)
      setTimeout(() => {
        navigate('/dashboard')
      }, 1200)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to accept invitation')
      setAccepting(false)
    }
  }

  return (
    <main className="login">
      <div className="login__card" style={{ maxWidth: 440 }}>
        <div className="sso-brand login__brand">
          <span className="sso-logo-badge" aria-hidden="true">
            <svg className="sso-logo-badge__svg" viewBox="0 0 24 24" fill="none">
              <path d="M4 19L12 4L20 19M7.5 14H16.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="square" />
            </svg>
          </span>
          <span className="sso-brand__meta">
            <span className="sso-brand__name">ARTracker</span>
            <span className="sso-brand__tag">Team Invitation</span>
          </span>
        </div>

        {loading ? (
          <p className="dash-empty" style={{ padding: '2rem 0' }}>
            Validating invitation…
          </p>
        ) : error ? (
          <div style={{ textAlign: 'center' }}>
            <h1 className="login__title" style={{ color: 'var(--color-error)' }}>
              Invitation Unavailable
            </h1>
            <p className="login__sub">{error}</p>
            <p className="login__foot" style={{ marginTop: '1.5rem' }}>
              <Link to="/login">Go to login page →</Link>
            </p>
          </div>
        ) : preview?.is_accepted ? (
          <div style={{ textAlign: 'center' }}>
            <h1 className="login__title">Already Accepted</h1>
            <p className="login__sub">
              This invitation link has already been claimed. If you are already a member, proceed to your console.
            </p>
            <p className="login__foot" style={{ marginTop: '1.5rem' }}>
              <Link to="/dashboard" className="dash-btn dash-btn--primary">
                Open Dashboard →
              </Link>
            </p>
          </div>
        ) : preview?.is_expired ? (
          <div style={{ textAlign: 'center' }}>
            <h1 className="login__title">Invitation Expired</h1>
            <p className="login__sub">
              This invitation link has expired. Please ask <strong>{preview.invited_by_name || 'the administrator'}</strong> to send a new invite.
            </p>
            <p className="login__foot" style={{ marginTop: '1.5rem' }}>
              <Link to="/login">Back to Sign In →</Link>
            </p>
          </div>
        ) : success ? (
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: '2.5rem', marginBottom: '0.5rem' }}>🎉</div>
            <h1 className="login__title">Welcome to {preview?.org_name}!</h1>
            <p className="login__sub">Your membership has been verified. Redirecting to console…</p>
          </div>
        ) : (
          <div>
            <h1 className="login__title">Join {preview?.org_name}</h1>
            <p className="login__sub">
              <strong>{preview?.invited_by_name || 'An administrator'}</strong> has invited you to collaborate as a{' '}
              <span className="badge badge--role">{preview?.role}</span> on ARTracker.
            </p>

            <div
              style={{
                margin: '1.25rem 0',
                padding: '0.875rem',
                background: 'var(--color-paper-2)',
                borderRadius: 'var(--radius-input)',
                border: '1px solid var(--color-rule)',
                fontSize: '0.8125rem',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.375rem' }}>
                <span style={{ color: 'var(--color-muted)' }}>Organization:</span>
                <strong>{preview?.org_name}</strong>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.375rem' }}>
                <span style={{ color: 'var(--color-muted)' }}>Assigned Role:</span>
                <span className="badge badge--preview">{preview?.role}</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                <span style={{ color: 'var(--color-muted)' }}>Invited Email:</span>
                <span>{preview?.email}</span>
              </div>
            </div>

            {authLoading ? (
              <p className="dash-empty">Checking authentication…</p>
            ) : user ? (
              <button
                type="button"
                className="dash-btn dash-btn--primary"
                style={{ width: '100%', padding: '0.625rem', fontSize: '0.875rem' }}
                onClick={() => void handleAccept()}
                disabled={accepting}
              >
                {accepting ? 'Joining team…' : `Accept & Join as ${user.email}`}
              </button>
            ) : (
              <a
                className="sso-provider-btn login__google"
                href={`${BASE}/auth/google/login`}
                style={{ width: '100%' }}
              >
                <span className="sso-provider-btn__left">
                  <span className="sso-provider-btn__icon" aria-hidden="true">
                    <GoogleIcon size={16} />
                  </span>
                  <span>Sign in with Google to Accept</span>
                </span>
                <span className="sso-provider-btn__right">
                  <svg className="sso-provider-btn__arrow" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                    <path d="M5 12h14m-5-5 5 5-5 5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </span>
              </a>
            )}

            <p className="login__foot" style={{ marginTop: '1.25rem' }}>
              <Link to="/">← Back to ARTracker home</Link>
            </p>
          </div>
        )}
      </div>
    </main>
  )
}
