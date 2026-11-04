import { useState, type FormEvent } from 'react'
import type { OrgMembership } from '../api'

const NEW = '__new__'

type Props = {
  orgs: OrgMembership[]
  activeId: number | null
  onChange: (id: number) => void
  onCreate: (name: string) => Promise<void>
}

export default function OrgSwitcher({ orgs, activeId, onChange, onCreate }: Props) {
  const [creating, setCreating] = useState(orgs.length === 0)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await onCreate(name)
      setName('')
      setCreating(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create organization')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="org-switcher">
      {orgs.length > 0 && (
        <select
          className="field"
          aria-label="Organization"
          value={creating ? NEW : (activeId ?? '')}
          onChange={(e) => {
            if (e.target.value === NEW) setCreating(true)
            else {
              setCreating(false)
              onChange(Number(e.target.value))
            }
          }}
        >
          {orgs.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name} ({o.role})
            </option>
          ))}
          <option value={NEW}>+ Create organization</option>
        </select>
      )}
      {creating && (
        <form className="inline-form" onSubmit={submit}>
          <input
            className="field"
            placeholder="Organization name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            autoFocus
          />
          <button className="btn btn-primary btn-sm" disabled={busy || !name.trim()}>
            Create
          </button>
          {orgs.length > 0 && (
            <button type="button" className="btn btn-secondary btn-sm" onClick={() => setCreating(false)}>
              Cancel
            </button>
          )}
        </form>
      )}
      {error && <p className="form-error">{error}</p>}
    </div>
  )
}
