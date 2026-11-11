import { useCallback, useEffect, useState } from 'react'
import {
  teamApi,
  type Organization,
  type OrganizationInvitation,
  type OrganizationMember,
  type Role,
} from '../api'
import Modal from './Modal'
import CustomSelect from './CustomSelect'

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
            <h2 className="dash-card__title">Team Collaborators</h2>
            <p className="dash-card__desc">
              Users with access to projects and telemetry in <strong>{org.name}</strong>.
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
              + Invite Teammate
            </button>
          )}
        </div>

        {loading ? (
          <p className="dash-empty" style={{ padding: 'var(--space-xl) var(--space-md)' }}>
            Loading collaborators…
          </p>
        ) : members.length === 0 ? (
          <p className="dash-empty" style={{ padding: 'var(--space-xl) var(--space-md)' }}>
            No members in this organization yet.
          </p>
        ) : (
          <div className="dash-table-wrap">
            <table className="dash-table">
              <thead>
                <tr>
                  <th>Collaborator</th>
                  <th>Role</th>
                  <th>Joined</th>
                  {canManage && <th className="team-action-cell">Actions</th>}
                </tr>
              </thead>
              <tbody>
                {members.map((m) => {
                  return (
                    <tr key={m.user_id}>
                      <td data-label="Collaborator">
                        <div className="team-user-cell">
                          {m.avatar_url ? (
                            <img
                              src={m.avatar_url}
                              alt=""
                              className="team-avatar"
                            />
                          ) : (
                            <span className="team-avatar team-avatar--fallback">
                              {(m.name || m.email)[0].toUpperCase()}
                            </span>
                          )}
                          <div className="team-member-info">
                            <strong className="team-member-name">
                              {m.name || m.email}
                            </strong>
                            <span className="team-member-email">
                              {m.email}
                            </span>
                          </div>
                        </div>
                      </td>
                      <td data-label="Role">
                        {isOwner && m.role !== 'owner' ? (
                          <div className="team-role-wrap">
                            <CustomSelect<'admin' | 'member' | 'viewer'>
                              size="sm"
                              options={[
                                { value: 'admin', label: 'Admin' },
                                { value: 'member', label: 'Member' },
                                { value: 'viewer', label: 'Viewer' },
                              ]}
                              value={m.role as 'admin' | 'member' | 'viewer'}
                              onChange={(val) => void handleUpdateRole(m.user_id, val)}
                              ariaLabel={`Role for ${m.name || m.email}`}
                            />
                          </div>
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
                        <td data-label="Actions" className="team-action-cell">
                          {m.role !== 'owner' && (
                            <button
                              type="button"
                              className="dash-btn"
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
            <span className="console-nav__badge">{invitations.length} Pending</span>
          </div>

          <div className="dash-table-wrap">
            <table className="dash-table">
              <thead>
                <tr>
                  <th>Invited Email</th>
                  <th>Role</th>
                  <th>Invited By</th>
                  <th>Expires</th>
                  <th className="team-action-cell">Action</th>
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
                    <td data-label="Action" className="team-action-cell">
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
          <div className="dash-form">
            <div className="team-invite-card">
              <strong className="team-invite-card__title">
                ✓ Invitation link generated!
              </strong>
              <p className="team-invite-card__desc">
                Share this link with your teammate. When they sign in with Google SSO, they will automatically join <strong>{org.name}</strong> as a <strong>{inviteRole}</strong>.
              </p>
            </div>

            <div className="alert-form-field">
              <label htmlFor="invite-generated-link" className="alert-form-field__label">
                Invite Link (Valid for 7 days)
              </label>
              <div className="team-invite-input-row">
                <input
                  id="invite-generated-link"
                  type="text"
                  readOnly
                  value={createdInviteUrl}
                  className="dash-input"
                  style={{ flex: 1 }}
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

            <div className="dash-actions">
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
          <form onSubmit={handleCreateInvite} className="dash-form">
            <div className="alert-form-field">
              <label htmlFor="invite-email" className="alert-form-field__label">
                Teammate Email Address
              </label>
              <input
                id="invite-email"
                type="email"
                required
                placeholder="name@company.com"
                className="dash-input"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
              />
            </div>

            <div className="alert-form-field">
              <label htmlFor="invite-role" className="alert-form-field__label">
                Assign Role
              </label>
              <CustomSelect<'admin' | 'member' | 'viewer'>
                id="invite-role"
                size="md"
                options={[
                  { value: 'member', label: 'Member', meta: 'Can view issues, telemetry, and replays' },
                  { value: 'admin', label: 'Admin', meta: 'Manage projects, API keys, and invites' },
                  { value: 'viewer', label: 'Viewer', meta: 'Read-only access to charts & telemetry' },
                ]}
                value={inviteRole}
                onChange={(val) => setInviteRole(val)}
                ariaLabel="Assign role"
              />
            </div>

            <div className="dash-actions">
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
          <div className="dash-form">
            <p className="dash-warning" role="alert">
              Are you sure you want to remove <strong>{memberToRemove.name || memberToRemove.email}</strong> from <strong>{org.name}</strong>? They will immediately lose access to all projects in this organization.
            </p>
            <div className="dash-actions">
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
                className="dash-btn dash-btn--danger"
                onClick={() => void handleRemoveMember()}
                disabled={removing}
              >
                {removing ? 'Removing…' : 'Remove Member'}
              </button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}
