import { useState, type FormEvent } from 'react'
import type { OrgMembership } from '../api'
import Modal from './Modal'
import Picker from './Picker'

type Props = {
  orgs: OrgMembership[]
  activeId: number | null
  onChange: (id: number) => void
  onCreate: (name: string) => Promise<void>
}

export default function OrgSwitcher({ orgs, activeId, onChange, onCreate }: Props) {
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const close = () => {
    setCreating(false)
    setName('')
    setError('')
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await onCreate(name)
      close()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create organization')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Picker
        caption="Organization"
        placeholder="Select organization"
        items={orgs.map((o) => ({ id: o.id, label: o.name, meta: o.role }))}
        activeId={activeId}
        onSelect={onChange}
        action={{ label: 'Create organization', onClick: () => setCreating(true) }}
      />
      <Modal open={creating} onClose={close} title="Create organization" description="Organizations group projects and members. You become its owner.">
        <form className="dash-form" onSubmit={submit}>
          <label className="dash-label" htmlFor="org-name">
            Name
          </label>
          <input id="org-name" className="dash-input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Inc." required autoFocus />
          {error && (
            <p className="dash-error" role="alert">
              {error}
            </p>
          )}
          <div className="dash-actions">
            <button type="button" className="key" onClick={close}>
              Cancel
            </button>
            <button className="key key--primary" disabled={busy || !name.trim()}>
              {busy ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </Modal>
    </>
  )
}
