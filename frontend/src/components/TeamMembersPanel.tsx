import { useCallback, useEffect, useState } from 'react'
import {
  teamApi,
  type Organization,
  type OrganizationInvitation,
  type OrganizationMember,
  type Role,
} from '../api'
import Modal from './Modal'

function formatAgo(timestamp: string): string {
  const diffMs = Date.now() - new Date(timestamp).getTime()
  if (Number.isNaN(diffMs)) return ''
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return `${Math.max(1, diffSec)}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return `${Math.floor(diffHr / 24)}d ago`
}

function formatExpiry(timestamp: string): string {
  const diffMs = new Date(timestamp).getTime() - Date.now()
  if (diffMs <= 0) return 'Expired'
  const days = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  if (days >= 1) return `in ${days} day${days > 1 ? 's' : ''}`
  const hours = Math.floor(diffMs / (1000 * 60 * 60))
  return `in ${Math.max(1, hours)} hour${hours > 1 ? 's' : ''}`
}

export default function TeamMembersPanel({
  org,
  canManage,
  currentUserRole,
}: {
  org: Organization
  canManage: boolean
  currentUserRole: Role
}) {
  const isOwner = currentUserRole === 'owner'
  const [members, setMembers] = useState<OrganizationMember[]>([])
  const [invitations, setInvitations] = useState<OrganizationInvitation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Invite Modal State
  const [showInviteModal, setShowInviteModal] = useState(false)
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState<'admin' | 'member' | 'viewer'>('member')
  const [inviting, setInviting] = useState(false)
  const [createdInviteUrl, setCreatedInviteUrl] = useState<string | null>(null)
  const [copiedLink, setCopiedLink] = useState(false)

  // Remove confirmation modal
  const [memberToRemove, setMemberToRemove] = useState<OrganizationMember | null>(null)
  const [removing, setRemoving] = useState(false)

  const loadTeam = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await teamApi.listMembers(org.id)
      setMembers(res.members)
      setInvitations(res.invitations)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load team members')
    } finally {
      setLoading(false)
    }
  }, [org.id])

  useEffect(() => {
    void loadTeam()
  }, [loadTeam])

  const handleCreateInvite = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!inviteEmail.trim()) return
    setInviting(true)
    setError(null)
    try {
      const res = await teamApi.createInvite(org.id, {
        email: inviteEmail.trim(),
        role: inviteRole,
      })
      setCreatedInviteUrl(res.invite_url)
      setInviteEmail('')
      void loadTeam()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create invitation')
    } finally {
      setInviting(false)
    }
  }

  const handleCopyLink = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url)
      setCopiedLink(true)
      setTimeout(() => setCopiedLink(false), 2000)
    } catch {
      // fallback
    }
  }

  const handleRevokeInvite = async (inviteId: number) => {
    try {
      await teamApi.revokeInvite(org.id, inviteId)
      setInvitations((prev) => prev.filter((i) => i.id !== inviteId))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke invitation')
    }
  }

  const handleUpdateRole = async (userId: number, newRole: 'admin' | 'member' | 'viewer') => {
    try {
      await teamApi.updateMemberRole(org.id, userId, newRole)
      setMembers((prev) =>
        prev.map((m) => (m.user_id === userId ? { ...m, role: newRole } : m)),
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to change role')
    }
  }

  const handleRemoveMember = async () => {
    if (!memberToRemove) return
    setRemoving(true)
    try {
      await teamApi.removeMember(org.id, memberToRemove.user_id)
      setMembers((prev) => prev.filter((m) => m.user_id !== memberToRemove.user_id))
      setMemberToRemove(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove member')
    } finally {
      setRemoving(false)
    }
  }

  return (
    <div className="dash-panel-stack">
      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {/* Active Team Members Card */}
      <section className="dash-card dash-card--compact">
        <div className="dash-card__head">
          <div>
            <h2 className="dash-card__title">Team Members</h2>
            <p className="dash-card__desc">
              Collaborators who have access to projects and telemetry in <strong>{org.name}</strong>.
            </p>
          </div>
          {canManage && (
            <button
              type="button"
              className="dash-btn dash-btn--primary"
              onClick={() => {
                setCreatedInviteUrl(null)
                setShowInviteModal(true)
              }}
            >
              + Invite Member
            </button>
          )}
        </div>

        {loading ? (
          <p className="dash-empty" style={{ padding: '2rem 1rem' }}>
            Loading members…
          </p>
        ) : members.length === 0 ? (
          <p className="dash-empty" style={{ padding: '2rem 1rem' }}>
            No members in this organization yet.
          </p>
        ) : (
          <div className="dash-table-wrap">
            <table className="dash-table">
              <thead>
                <tr>
                  <th>Member</th>
                  <th>Role</th>
                  <th>Joined</th>
                  {canManage && <th style={{ textAlign: 'right' }}>Actions</th>}
                </tr>
              </thead>
              <tbody>
                {members.map((m) => {
                  return (
                    <tr key={m.user_id}>
                      <td data-label="Member">
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.625rem' }}>
                          {m.avatar_url ? (
                            <img
                              src={m.avatar_url}
                              alt=""
                              style={{ width: 28, height: 28, borderRadius: '50%' }}
                            />
                          ) : (
                            <span
                              style={{
                                width: 28,
                                height: 28,
                                borderRadius: '50%',
                                background: 'var(--color-paper-3)',
                                display: 'inline-flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                fontSize: '0.75rem',
                                fontWeight: 600,
                              }}
                            >
                              {(m.name || m.email)[0].toUpperCase()}
                            </span>
                          )}
                          <div>
                            <strong style={{ display: 'block', color: 'var(--color-ink)' }}>
                              {m.name || m.email}
                            </strong>
                            <span className="dash-cell-sub" style={{ fontSize: '0.75rem' }}>
                              {m.email}
                            </span>
                          </div>
                        </div>
                      </td>
                      <td data-label="Role">
                        {isOwner && m.role !== 'owner' ? (
                          <select
                            className="dash-input"
                            style={{ padding: '0.2rem 0.5rem', fontSize: '0.75rem' }}
                            value={m.role}
                            onChange={(e) =>
                              void handleUpdateRole(
                                m.user_id,
                                e.target.value as 'admin' | 'member' | 'viewer',
                              )
                            }
                          >
                            <option value="admin">Admin</option>
                            <option value="member">Member</option>
                            <option value="viewer">Viewer</option>
                          </select>
                        ) : (
                          <span
                            className={`badge ${
                              m.role === 'owner'
                                ? 'badge--active'
                                : m.role === 'admin'
                                  ? 'badge--role'
                                  : 'badge--preview'
                            }`}
                          >
                            {m.role}
                          </span>
                        )}
                      </td>
                      <td data-label="Joined">
                        <span className="dash-code">{formatAgo(m.joined_at)}</span>
                      </td>
                      {canManage && (
                        <td data-label="Actions" style={{ textAlign: 'right' }}>
                          {m.role !== 'owner' && (
                            <button
                              type="button"
                              className="dash-btn"
                              style={{ color: 'var(--color-error)', borderColor: 'rgba(239, 68, 68, 0.2)' }}
                              onClick={() => setMemberToRemove(m)}
                            >
                              Remove
                            </button>
                          )}
                        </td>
                      )}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Pending Invitations Card */}
      {canManage && invitations.length > 0 && (
        <section className="dash-card dash-card--compact">
          <div className="dash-card__head">
            <div>
              <h2 className="dash-card__title">Pending Invitations</h2>
              <p className="dash-card__desc">
                Invite links awaiting acceptance via Google SSO.
              </p>
            </div>
          </div>

          <div className="dash-table-wrap">
            <table className="dash-table">
              <thead>
                <tr>
                  <th>Invited Email</th>
                  <th>Role</th>
                  <th>Invited By</th>
                  <th>Expires</th>
                  <th style={{ textAlign: 'right' }}>Action</th>
                </tr>
              </thead>
              <tbody>
                {invitations.map((inv) => (
                  <tr key={inv.id}>
                    <td data-label="Email">
                      <strong>{inv.email}</strong>
                    </td>
                    <td data-label="Role">
                      <span className="badge badge--role">{inv.role}</span>
                    </td>
                    <td data-label="Invited By">
                      <span className="dash-cell-sub">{inv.invited_by_name || 'Admin'}</span>
                    </td>
                    <td data-label="Expires">
                      <span className="dash-code">{formatExpiry(inv.expires_at)}</span>
                    </td>
                    <td data-label="Action" style={{ textAlign: 'right' }}>
                      <button
                        type="button"
                        className="dash-btn"
                        onClick={() => void handleRevokeInvite(inv.id)}
                      >
                        Revoke
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {/* Modal Invite Member */}
      <Modal
        open={showInviteModal}
        title="Invite Team Member"
        onClose={() => {
          setShowInviteModal(false)
          setCreatedInviteUrl(null)
        }}
      >
          {createdInviteUrl ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div
                style={{
                  padding: '0.875rem',
                  background: 'var(--color-accent-wash)',
                  borderRadius: 'var(--radius-input)',
                  border: '1px solid var(--color-accent)',
                }}
              >
                <strong style={{ display: 'block', color: 'var(--color-ink)', marginBottom: '0.25rem' }}>
                  ✓ Invitation link generated!
                </strong>
                <p style={{ margin: 0, fontSize: '0.8125rem', color: 'var(--color-ink-2)' }}>
                  Share this link with your teammate. When they sign in with Google, they will automatically join <strong>{org.name}</strong> as a <strong>{inviteRole}</strong>.
                </p>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, marginBottom: '0.375rem' }}>
                  Invite Link (Valid for 7 days)
                </label>
                <div style={{ display: 'flex', gap: '0.5rem' }}>
                  <input
                    type="text"
                    readOnly
                    value={createdInviteUrl}
                    className="dash-input"
                    style={{ flex: 1, fontFamily: 'monospace', fontSize: '0.8125rem' }}
                    onClick={(e) => (e.target as HTMLInputElement).select()}
                  />
                  <button
                    type="button"
                    className="dash-btn dash-btn--primary"
                    onClick={() => void handleCopyLink(createdInviteUrl)}
                  >
                    {copiedLink ? 'Copied!' : 'Copy Link'}
                  </button>
                </div>
              </div>

              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '0.5rem' }}>
                <button
                  type="button"
                  className="dash-btn"
                  onClick={() => {
                    setShowInviteModal(false)
                    setCreatedInviteUrl(null)
                  }}
                >
                  Done
                </button>
              </div>
            </div>
          ) : (
            <form onSubmit={handleCreateInvite} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label htmlFor="invite-email" style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, marginBottom: '0.375rem' }}>
                  Teammate Email Address
                </label>
                <input
                  id="invite-email"
                  type="email"
                  required
                  placeholder="name@company.com"
                  className="dash-input"
                  style={{ width: '100%' }}
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                />
              </div>

              <div>
                <label htmlFor="invite-role" style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, marginBottom: '0.375rem' }}>
                  Assign Role
                </label>
                <select
                  id="invite-role"
                  className="dash-input"
                  style={{ width: '100%' }}
                  value={inviteRole}
                  onChange={(e) => setInviteRole(e.target.value as 'admin' | 'member' | 'viewer')}
                >
                  <option value="member">Member — Can manage issues, funnels, and recordings</option>
                  <option value="admin">Admin — Full project & SDK key management</option>
                  <option value="viewer">Viewer — Read-only access to charts & telemetry</option>
                </select>
              </div>

              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '0.5rem' }}>
                <button
                  type="button"
                  className="dash-btn"
                  onClick={() => setShowInviteModal(false)}
                  disabled={inviting}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="dash-btn dash-btn--primary"
                  disabled={inviting || !inviteEmail.trim()}
                >
                  {inviting ? 'Generating…' : 'Generate Invite Link'}
                </button>
              </div>
            </form>
          )}
        </Modal>

      {/* Modal Remove Member Confirmation */}
      <Modal
        open={Boolean(memberToRemove)}
        title="Remove Team Member"
        onClose={() => setMemberToRemove(null)}
      >
        {memberToRemove && (
          <>
            <p style={{ margin: '0 0 1.25rem 0', fontSize: '0.875rem' }}>
              Are you sure you want to remove <strong>{memberToRemove.name || memberToRemove.email}</strong> from <strong>{org.name}</strong>? They will immediately lose access to all projects in this organization.
            </p>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem' }}>
              <button
                type="button"
                className="dash-btn"
                onClick={() => setMemberToRemove(null)}
                disabled={removing}
              >
                Cancel
              </button>
              <button
                type="button"
                className="dash-btn"
                style={{ background: 'var(--color-error)', color: '#fff', borderColor: 'transparent' }}
                onClick={() => void handleRemoveMember()}
                disabled={removing}
              >
                {removing ? 'Removing…' : 'Remove Member'}
              </button>
            </div>
          </>
        )}
      </Modal>
    </div>
  )
}
