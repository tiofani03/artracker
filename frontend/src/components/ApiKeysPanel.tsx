import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { keyApi, type ApiKey, type CreatedKey, type KeyType, type Project } from '../api'
import Modal from './Modal'
import { useToast } from './Toast'

const TYPES: { value: KeyType; title: string; desc: string; prefix: string }[] = [
  { value: 'public_client', title: 'Public Client Key', desc: 'Safe to embed in client SDKs (Web, React, Android, iOS).', prefix: 'art_live_…' },
  { value: 'secret_admin', title: 'Secret Admin Key', desc: 'Server-side ingestion and management only. Never ship in client apps.', prefix: 'art_sec_…' },
]
const TYPE_LABEL: Record<KeyType, string> = { public_client: 'Public Client', secret_admin: 'Secret Admin', mcp: 'MCP' }

type Props = { project: Project; canManage: boolean; canManageMcp?: boolean }

export default function ApiKeysPanel({ project, canManage, canManageMcp = false }: Props) {
  const toast = useToast()
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [fresh, setFresh] = useState<CreatedKey | null>(null)
  const [generating, setGenerating] = useState(false)
  const [newType, setNewType] = useState<KeyType>('public_client')
  const [busy, setBusy] = useState(false)
  const [modalError, setModalError] = useState('')
  const [confirmId, setConfirmId] = useState<number | null>(null)

  const load = useCallback(async () => {
    try {
      setKeys((await keyApi.list(project.id)).items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load keys')
    } finally {
      setLoading(false)
    }
  }, [project.id])

  useEffect(() => {
    void load()
  }, [load])

  const closeModal = () => {
    setGenerating(false)
    setFresh(null)
    setModalError('')
  }

  const generate = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setModalError('')
    try {
      setFresh(await keyApi.create(project.id, newType))
      await load()
    } catch (err) {
      setModalError(err instanceof Error ? err.message : 'Failed to generate key')
    } finally {
      setBusy(false)
    }
  }

  const rotate = async (id: number) => {
    setError('')
    try {
      setFresh(await keyApi.rotate(project.id, id))
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rotate key')
    }
  }

  const revoke = async (id: number) => {
    setError('')
    try {
      await keyApi.revoke(project.id, id)
      setConfirmId(null)
      await load()
      toast('Key revoked')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke key')
    }
  }

  const copy = async () => {
    if (!fresh) return
    try {
      await navigator.clipboard.writeText(fresh.secret)
      toast('Copied to clipboard')
    } catch {
      toast('Copy failed. Select the key and copy it manually.', 'error')
    }
  }

  const showActions = canManage || (canManageMcp && keys.some((k) => k.key_type === 'mcp'))
  const modalOpen = generating || fresh !== null

  return (
    <div className="dash-panel-stack">
      <section className="dash-card dash-card--compact" aria-labelledby="keys-title">
        <div className="dash-card__head">
          <div>
            <h2 id="keys-title" className="dash-card__title">
              Project API Keys
            </h2>
            <p className="dash-card__desc">Manage API credentials for {project.name}.</p>
          </div>
          {canManage && (
            <button
              type="button"
              className="dash-btn dash-btn--primary"
              onClick={() => {
                setNewType('public_client')
                setGenerating(true)
              }}
            >
              + Generate New Key
            </button>
          )}
        </div>

        {error && (
          <p className="dash-error" role="alert">
            {error}
          </p>
        )}

        {loading ? (
          <p className="dash-empty" style={{ padding: 'var(--space-xl) var(--space-md)' }}>
            Loading API keys…
          </p>
        ) : keys.length === 0 ? (
          <p className="dash-empty" style={{ padding: 'var(--space-xl) var(--space-md)' }}>
            No API keys found for this project.{canManage ? ' Click Generate New Key above to connect an SDK.' : ''}
          </p>
        ) : (
          <div className="dash-table-wrap">
            <table className="dash-table">
              <thead>
                <tr>
                  <th scope="col">Key Identifier</th>
                  <th scope="col">Scope / Type</th>
                  <th scope="col">Status</th>
                  <th scope="col">Created Date</th>
                  {showActions && (
                    <th scope="col" style={{ textAlign: 'right' }}>
                      Actions
                    </th>
                  )}
                </tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <tr key={k.id} className={k.is_active ? '' : 'is-revoked'}>
                    <td data-label="Key Identifier">
                      <code className="dash-code">{k.prefix}…</code>
                    </td>
                    <td data-label="Type">
                      <span className={`badge ${k.key_type === 'public_client' ? 'badge--active' : 'badge--role'}`}>
                        {TYPE_LABEL[k.key_type]}
                      </span>
                    </td>
                    <td data-label="Status">
                      <span className={`badge ${k.is_active ? 'badge--active' : 'badge--revoked'}`}>
                        {k.is_active ? 'Active' : 'Revoked'}
                      </span>
                    </td>
                    <td data-label="Created">{new Date(k.created_at).toLocaleDateString()}</td>
                    {showActions && (
                      <td className="dash-table__cell-actions">
                        <div className="dash-table__actions">
                          {k.is_active && (canManage || k.key_type === 'mcp') &&
                            (confirmId === k.id ? (
                              <>
                                <button type="button" className="dash-btn dash-btn--danger" onClick={() => void revoke(k.id)}>
                                  Confirm Revoke
                                </button>
                                <button type="button" className="dash-btn" onClick={() => setConfirmId(null)}>
                                  Cancel
                                </button>
                              </>
                            ) : (
                              <>
                                {canManage && (
                                  <button type="button" className="dash-btn" onClick={() => void rotate(k.id)}>
                                    Rotate
                                  </button>
                                )}
                                <button type="button" className="dash-btn" onClick={() => setConfirmId(k.id)}>
                                  Revoke
                                </button>
                              </>
                            ))}
                        </div>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <Modal
        open={modalOpen}
        onClose={closeModal}
        title={fresh ? 'Your new API key' : 'Generate API key'}
        description={fresh ? undefined : `For project ${project.name}.`}
      >
        {fresh ? (
          <div className="dash-form">
            <p className="dash-warning" role="note">
              Copy this secret key now. For security reasons it will never be displayed again.
            </p>
            <div className="dash-secret">
              <code>{fresh.secret}</code>
            </div>
            <div className="dash-actions">
              <button type="button" className="dash-btn" onClick={copy}>
                Copy to Clipboard
              </button>
              <button type="button" className="dash-btn dash-btn--primary" onClick={closeModal}>
                Done
              </button>
            </div>
          </div>
        ) : (
          <form className="dash-form" onSubmit={generate}>
            <span className="dash-label" id="key-type-label">
              Select Key Scope
            </span>
            <div className="dash-choice-col" role="radiogroup" aria-labelledby="key-type-label">
              {TYPES.map((t) => (
                <label key={t.value} className={`dash-choice dash-choice--block${newType === t.value ? ' is-on' : ''}`}>
                  <input type="radio" name="key-type" value={t.value} checked={newType === t.value} onChange={() => setNewType(t.value)} />
                  <span>
                    <strong>
                      {t.title} <code className="dash-code">{t.prefix}</code>
                    </strong>
                    <span className="dash-choice__desc">{t.desc}</span>
                  </span>
                </label>
              ))}
            </div>
            {modalError && (
              <p className="dash-error" role="alert">
                {modalError}
              </p>
            )}
            <div className="dash-actions">
              <button type="button" className="dash-btn" onClick={closeModal}>
                Cancel
              </button>
              <button type="submit" className="dash-btn dash-btn--primary" disabled={busy}>
                {busy ? 'Generating…' : 'Generate Key'}
              </button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  )
}
