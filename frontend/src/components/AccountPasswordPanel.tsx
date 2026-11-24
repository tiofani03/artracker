import { useState, type FormEvent } from 'react'
import { authApi, type User } from '../api'
import { useToast } from './Toast'

type Props = {
  user: User
  onUpdated: () => Promise<void>
}

/** Form for authenticated users to set an initial password or change their existing password. */
export default function AccountPasswordPanel({ user, onUpdated }: Props) {
  const toast = useToast()
  const hasPassword = Boolean(user.has_password)

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [statusMessage, setStatusMessage] = useState('')

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setStatusMessage('')

    const byteLen = new TextEncoder().encode(newPassword).length
    if (byteLen < 10 || byteLen > 72) {
      setError('Password must be 10 to 72 bytes long.')
      return
    }
    if (newPassword !== confirmPassword) {
      setError('New password and confirmation do not match.')
      return
    }
    if (hasPassword && newPassword === currentPassword) {
      setError('New password must be different from current password.')
      return
    }

    setSubmitting(true)
    try {
      await authApi.localChangePassword(hasPassword ? currentPassword : '', newPassword)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      await onUpdated()
      const msg = hasPassword ? 'Password updated.' : 'Password set. You can now sign in with email and password.'
      setStatusMessage(msg)
      toast(msg)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update password.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="dash-card">
      <div className="dash-card__head">
        <div>
          <h2 className="dash-card__title">{hasPassword ? 'Change Password' : 'Set Password'}</h2>
          <p className="dash-card__desc">
            {hasPassword
              ? `Update the password for ${user.email}. Other active sessions will be signed out.`
              : `Add a password to ${user.email} so you can also sign in with email and password. Google sign-in will keep working.`}
          </p>
        </div>
      </div>

      <form className="dash-form" style={{ maxWidth: '420px', marginTop: 'var(--space-sm)' }} onSubmit={(e) => void handleSubmit(e)}>
        {error && (
          <p className="dash-error" role="alert">
            {error}
          </p>
        )}
        {statusMessage && (
          <p className="dash-note" role="status" style={{ color: 'var(--color-ink)' }}>
            {statusMessage}
          </p>
        )}

        {hasPassword && (
          <>
            <label className="dash-label" htmlFor="account-current-password">
              Current Password
            </label>
            <input
              id="account-current-password"
              className="dash-input"
              type="password"
              autoComplete="current-password"
              required
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              disabled={submitting}
            />
          </>
        )}

        <label className="dash-label" htmlFor="account-new-password">
          New Password
        </label>
        <input
          id="account-new-password"
          className="dash-input"
          type="password"
          autoComplete="new-password"
          minLength={10}
          maxLength={72}
          required
          value={newPassword}
          onChange={(e) => setNewPassword(e.target.value)}
          disabled={submitting}
        />
        <p className="dash-note">10 to 72 characters.</p>

        <label className="dash-label" htmlFor="account-confirm-password">
          Confirm New Password
        </label>
        <input
          id="account-confirm-password"
          className="dash-input"
          type="password"
          autoComplete="new-password"
          minLength={10}
          maxLength={72}
          required
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          disabled={submitting}
        />

        <div className="dash-actions" style={{ justifyContent: 'flex-start' }}>
          <button type="submit" className="dash-btn dash-btn--primary" disabled={submitting}>
            {submitting ? 'Saving…' : hasPassword ? 'Update Password' : 'Set Password'}
          </button>
        </div>
      </form>
    </section>
  )
}
