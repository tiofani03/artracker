import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { keyApi, type ApiKey, type CreatedKey, type KeyType, type Project } from '../api'
import Modal from './Modal'
import { useToast } from './Toast'

const TYPES: { value: KeyType; title: string; desc: string; prefix: string }[] = [
  { value: 'public_client', title: 'Public', desc: 'Safe to embed in SDK clients (web, Android, iOS).', prefix: 'art_live_…' },
  { value: 'secret_admin', title: 'Secret', desc: 'Server-side only. Never ship it in a client app.', prefix: 'art_sec_…' },
]
const TYPE_LABEL: Record<KeyType, string> = { public_client: 'Public', secret_admin: 'Secret' }

type Props = { project: Project; canManage: boolean }

export default function ApiKeysPanel({ project, canManage }: Props) {
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

  // The parent remounts this panel per project (key=project.id), so state starts fresh.
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

  const modalOpen = generating || fresh !== null

  return (
    <section className="dash-card" aria-labelledby="keys-title">
      <div className="dash-card__head">
        <div>
          <h2 id="keys-title" className="dash-card__title">
            API keys
          </h2>
          <p className="dash-card__desc">Authenticate the ARTracker SDK for {project.name}.</p>
        </div>
        {canManage && (
          <button
            className="key key--primary"
            onClick={() => {
              setNewType('public_client')
              setGenerating(true)
            }}
          >
            Generate key
          </button>
        )}
      </div>

      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <p className="dash-empty">Loading keys…</p>
      ) : keys.length === 0 ? (
        <p className="dash-empty">No API keys yet.{canManage ? ' Generate one to connect an SDK.' : ''}</p>
      ) : (
        <div className="dash-table-wrap">
          <table className="dash-table">
            <thead>
              <tr>
                <th scope="col">Key</th>
                <th scope="col">Type</th>
                <th scope="col">Status</th>
                <th scope="col">Created</th>
                {canManage && (
                  <th scope="col">
                    <span className="sr-only">Actions</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id} className={k.is_active ? '' : 'is-revoked'}>
                  <td data-label="Key">
                    <code className="dash-code">{k.prefix}…</code>
                  </td>
                  <td data-label="Type">{TYPE_LABEL[k.key_type]}</td>
                  <td data-label="Status">
                    <span className={`badge ${k.is_active ? 'badge--active' : 'badge--revoked'}`}>{k.is_active ? 'Active' : 'Revoked'}</span>
                  </td>
                  <td data-label="Created">{new Date(k.created_at).toLocaleDateString()}</td>
                  {canManage && (
                    <td className="dash-table__cell-actions">
                      <div className="dash-table__actions">
                      {k.is_active &&
                        (confirmId === k.id ? (
                          <>
                            <button className="dash-btn dash-btn--danger" onClick={() => revoke(k.id)}>
                              Confirm revoke
                            </button>
                            <button className="dash-btn" onClick={() => setConfirmId(null)}>
                              Cancel
                            </button>
                          </>
                        ) : (
                          <>
                            <button className="dash-btn" onClick={() => rotate(k.id)}>
                              Rotate
                            </button>
                            <button className="dash-btn" onClick={() => setConfirmId(k.id)}>
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

      <Modal
        open={modalOpen}
        onClose={closeModal}
        title={fresh ? 'Your new API key' : 'Generate API key'}
        description={fresh ? undefined : `For project ${project.name}.`}
      >
        {fresh ? (
          <div className="dash-form">
            <p className="dash-warning" role="note">
              Copy this key now. For security it will not be shown again.
            </p>
            <div className="dash-secret">
              <code>{fresh.secret}</code>
            </div>
            <div className="dash-actions">
              <button type="button" className="key" onClick={copy}>
                Copy to clipboard
              </button>
              <button type="button" className="key key--primary" onClick={closeModal}>
                Done
              </button>
            </div>
          </div>
        ) : (
          <form className="dash-form" onSubmit={generate}>
            <span className="dash-label" id="key-type-label">
              Key type
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
              <button type="button" className="key" onClick={closeModal}>
                Cancel
              </button>
              <button className="key key--primary" disabled={busy}>
                {busy ? 'Generating…' : 'Generate'}
              </button>
            </div>
          </form>
        )}
      </Modal>
    </section>
  )
}
