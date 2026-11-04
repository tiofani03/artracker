import { useState, type FormEvent } from 'react'
import type { Platform, Project } from '../api'
import Modal from './Modal'
import Picker from './Picker'

type Props = {
  projects: Project[]
  activeId: number | null
  onChange: (id: number) => void
  canManage: boolean
  onCreate: (input: { name: string; platform: Platform }) => Promise<void>
}

const PLATFORMS: { value: Platform; label: string }[] = [
  { value: 'web', label: 'Web' },
  { value: 'android', label: 'Android' },
  { value: 'ios', label: 'iOS' },
]

export default function ProjectSelector({ projects, activeId, onChange, canManage, onCreate }: Props) {
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [platform, setPlatform] = useState<Platform>('web')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const close = () => {
    setCreating(false)
    setName('')
    setPlatform('web')
    setError('')
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await onCreate({ name, platform })
      close()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Picker
        caption="Project"
        placeholder="No projects"
        items={projects.map((p) => ({ id: p.id, label: p.name, meta: p.platform }))}
        activeId={activeId}
        onSelect={onChange}
        action={canManage ? { label: 'New project', onClick: () => setCreating(true) } : undefined}
      />
      <Modal open={creating} onClose={close} title="New project" description="Each project has its own API keys.">
        <form className="dash-form" onSubmit={submit}>
          <label className="dash-label" htmlFor="project-name">
            Name
          </label>
          <input id="project-name" className="dash-input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Checkout app" required autoFocus />
          <span className="dash-label" id="project-platform">
            Platform
          </span>
          <div className="dash-choice-row" role="radiogroup" aria-labelledby="project-platform">
            {PLATFORMS.map((p) => (
              <label key={p.value} className={`dash-choice${platform === p.value ? ' is-on' : ''}`}>
                <input type="radio" name="platform" value={p.value} checked={platform === p.value} onChange={() => setPlatform(p.value)} />
                {p.label}
              </label>
            ))}
          </div>
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
              {busy ? 'Creating…' : 'Create project'}
            </button>
          </div>
        </form>
      </Modal>
    </>
  )
}
