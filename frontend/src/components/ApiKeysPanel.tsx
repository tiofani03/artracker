import { useCallback, useEffect, useState } from 'react'
import { keyApi, type ApiKey, type CreatedKey, type KeyType, type Project } from '../api'

const LABEL: Record<KeyType, string> = { public_client: 'Public (SDK client)', secret_admin: 'Secret (server)' }

type Props = { project: Project; canManage: boolean }

export default function ApiKeysPanel({ project, canManage }: Props) {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [fresh, setFresh] = useState<CreatedKey | null>(null)
  const [copied, setCopied] = useState(false)
  const [confirmId, setConfirmId] = useState<number | null>(null)
  const [newType, setNewType] = useState<KeyType>('public_client')

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

  const run = async (action: () => Promise<CreatedKey | ApiKey>) => {
    setError('')
    try {
      const result = await action()
      if ('secret' in result) {
        setFresh(result)
        setCopied(false)
      }
      setConfirmId(null)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    }
  }

  const copy = async () => {
    if (!fresh) return
    try {
      await navigator.clipboard.writeText(fresh.secret)
      setCopied(true)
    } catch {
      setError('Copy failed: select the key and copy it manually')
    }
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>API keys</h2>
        {canManage && (
          <div className="inline-form">
            <select className="field" aria-label="Key type" value={newType} onChange={(e) => setNewType(e.target.value as KeyType)}>
              <option value="public_client">Public (art_live_…)</option>
              <option value="secret_admin">Secret (art_sec_…)</option>
            </select>
            <button className="btn btn-primary btn-sm" onClick={() => run(() => keyApi.create(project.id, newType))}>
              Create key
            </button>
          </div>
        )}
      </div>

      {fresh && (
        <div className="secret-banner" role="status">
          <p>Copy this key now. It will not be shown again.</p>
          <div className="inline-form">
            <code className="secret-value">{fresh.secret}</code>
            <button className="btn btn-secondary btn-sm" onClick={copy}>
              {copied ? 'Copied' : 'Copy'}
            </button>
            <button className="btn btn-secondary btn-sm" onClick={() => setFresh(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}

      {error && <p className="form-error">{error}</p>}

      {loading ? (
        <p className="muted">Loading…</p>
      ) : keys.length === 0 ? (
        <p className="muted">No keys yet.</p>
      ) : (
        <table className="key-table">
          <thead>
            <tr>
              <th>Key</th>
              <th>Type</th>
              <th>Status</th>
              {canManage && <th />}
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id} className={k.is_active ? '' : 'revoked'}>
                <td>
                  <code>{k.prefix}…</code>
                </td>
                <td>{LABEL[k.key_type]}</td>
                <td>{k.is_active ? 'Active' : 'Revoked'}</td>
                {canManage && (
                  <td className="row-actions">
                    {k.is_active &&
                      (confirmId === k.id ? (
                        <>
                          <button className="btn btn-sm btn-danger" onClick={() => run(() => keyApi.revoke(project.id, k.id))}>
                            Confirm revoke
                          </button>
                          <button className="btn btn-secondary btn-sm" onClick={() => setConfirmId(null)}>
                            Cancel
                          </button>
                        </>
                      ) : (
                        <>
                          <button className="btn btn-secondary btn-sm" onClick={() => run(() => keyApi.rotate(project.id, k.id))}>
                            Rotate
                          </button>
                          <button className="btn btn-secondary btn-sm" onClick={() => setConfirmId(k.id)}>
                            Revoke
                          </button>
                        </>
                      ))}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
