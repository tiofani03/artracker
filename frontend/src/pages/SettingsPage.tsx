import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { orgApi, type Platform, type Project } from '../api'
import { useAuth } from '../auth/AuthContext'
import ApiKeysPanel from '../components/ApiKeysPanel'
import OrgSwitcher from '../components/OrgSwitcher'
import { Link, Redirect } from '../router'

const ORG_KEY = 'artracker.org'

function readStoredOrg(): number | null {
  try {
    const v = localStorage.getItem(ORG_KEY)
    return v ? Number(v) : null
  } catch {
    return null
  }
}

function storeOrg(id: number) {
  try {
    localStorage.setItem(ORG_KEY, String(id))
  } catch {
    // storage unavailable (private mode): selection just won't persist
  }
}

export default function SettingsPage() {
  const { user, loading, logout, reload } = useAuth()
  const [storedOrg, setStoredOrg] = useState<number | null>(readStoredOrg)
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [newName, setNewName] = useState('')
  const [newPlatform, setNewPlatform] = useState<Platform>('web')

  const orgs = user?.orgs ?? []
  const activeOrg = orgs.find((o) => o.id === storedOrg) ?? orgs[0] ?? null
  const activeOrgId = activeOrg?.id ?? null
  const canManage = activeOrg?.role === 'owner' || activeOrg?.role === 'admin'

  const loadProjects = useCallback(async (orgId: number) => {
    try {
      const items = (await orgApi.listProjects(orgId)).items
      setProjects(items)
      setProjectId((cur) => (items.some((p) => p.id === cur) ? cur : (items[0]?.id ?? null)))
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load projects')
    }
  }, [])

  useEffect(() => {
    if (activeOrgId !== null) void loadProjects(activeOrgId)
  }, [activeOrgId, loadProjects])

  if (loading) return null
  if (!user) return <Redirect to="/login" />

  const project = activeOrg ? (projects.find((p) => p.id === projectId) ?? null) : null

  const selectOrg = (id: number) => {
    storeOrg(id)
    setStoredOrg(id)
  }

  const createOrg = async (name: string) => {
    const org = await orgApi.create(name)
    selectOrg(org.id)
    await reload()
  }

  const createProject = async (e: FormEvent) => {
    e.preventDefault()
    if (!activeOrg) return
    try {
      const p = await orgApi.createProject(activeOrg.id, { name: newName, platform: newPlatform })
      setNewName('')
      setProjectId(p.id)
      await loadProjects(activeOrg.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project')
    }
  }

  return (
    <main className="settings">
      <header className="settings-head">
        <Link to="/" className="brand">
          <div className="brand-icon">A</div>
          <span>ARTracker</span>
        </Link>
        <div className="settings-user">
          <span className="muted">{user.email}</span>
          <button
            className="btn btn-secondary btn-sm"
            onClick={async () => {
              await logout()
            }}
          >
            Log out
          </button>
        </div>
      </header>

      <h1>Settings</h1>

      <section className="panel">
        <div className="panel-head">
          <h2>Organization</h2>
        </div>
        <OrgSwitcher orgs={orgs} activeId={activeOrgId} onChange={selectOrg} onCreate={createOrg} />
      </section>

      {error && <p className="form-error">{error}</p>}

      {activeOrg && (
        <section className="panel">
          <div className="panel-head">
            <h2>Project</h2>
            {projects.length > 0 && (
              <select className="field" aria-label="Project" value={projectId ?? ''} onChange={(e) => setProjectId(Number(e.target.value))}>
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} · {p.platform}
                  </option>
                ))}
              </select>
            )}
          </div>
          {projects.length === 0 && <p className="muted">No projects yet.</p>}
          {canManage ? (
            <form className="inline-form" onSubmit={createProject}>
              <input className="field" placeholder="New project name" value={newName} onChange={(e) => setNewName(e.target.value)} required />
              <select className="field" aria-label="Platform" value={newPlatform} onChange={(e) => setNewPlatform(e.target.value as Platform)}>
                <option value="web">Web</option>
                <option value="android">Android</option>
                <option value="ios">iOS</option>
              </select>
              <button className="btn btn-primary btn-sm" disabled={!newName.trim()}>
                New project
              </button>
            </form>
          ) : (
            <p className="muted">Your role ({activeOrg.role}) is read-only for projects and keys.</p>
          )}
        </section>
      )}

      {project && <ApiKeysPanel key={project.id} project={project} canManage={canManage} />}
    </main>
  )
}
