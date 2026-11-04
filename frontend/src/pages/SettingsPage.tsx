import { useCallback, useEffect, useState } from 'react'
import { orgApi, type Platform, type Project } from '../api'
import { useAuth } from '../auth/AuthContext'
import ApiKeysPanel from '../components/ApiKeysPanel'
import OrgSwitcher from '../components/OrgSwitcher'
import ProjectSelector from '../components/ProjectSelector'
import { ToastProvider } from '../components/Toast'
import { Link, Redirect } from '../router'
import '../dashboard.css'

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

  const createProject = async (input: { name: string; platform: Platform }) => {
    if (!activeOrg) return
    const p = await orgApi.createProject(activeOrg.id, input)
    setProjectId(p.id)
    await loadProjects(activeOrg.id)
  }

  return (
    <ToastProvider>
      <div className="dash">
        <header className="dash-bar">
          <div className="dash-bar__inner">
            <Link to="/" className="dash-brand" aria-label="ARTracker home">
              <span className="sso-logo-badge" aria-hidden="true">
                <svg className="sso-logo-badge__svg" viewBox="0 0 24 24" fill="none">
                  <path d="M4 19L12 4L20 19M7.5 14H16.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="square" />
                </svg>
              </span>
              <span className="dash-brand__name">ARTracker</span>
            </Link>
            <div className="dash-bar__pickers">
              <OrgSwitcher orgs={orgs} activeId={activeOrgId} onChange={selectOrg} onCreate={createOrg} />
              {activeOrg && (
                <ProjectSelector projects={projects} activeId={projectId} onChange={setProjectId} canManage={canManage} onCreate={createProject} />
              )}
            </div>
            <div className="dash-bar__user">
              <span className="dash-bar__email">{user.email}</span>
              <button className="key" onClick={() => void logout()}>
                Log out
              </button>
            </div>
          </div>
        </header>

        <main className="dash-main">
          <div className="dash-title">
            <h1>Settings</h1>
            {activeOrg && (
              <p>
                {activeOrg.name} · <span className="badge badge--role">{activeOrg.role}</span>
              </p>
            )}
          </div>

          {error && (
            <p className="dash-error" role="alert">
              {error}
            </p>
          )}

          {!activeOrg && <p className="dash-empty dash-card">Create an organization from the switcher above to get started.</p>}

          {activeOrg && !project && !error && (
            <p className="dash-empty dash-card">
              No projects yet.{' '}
              {canManage ? 'Use the project menu above to create one.' : `Your role (${activeOrg.role}) cannot create projects.`}
            </p>
          )}

          {activeOrg && !canManage && project && (
            <p className="dash-note">Your role ({activeOrg.role}) is read-only for projects and keys.</p>
          )}

          {project && <ApiKeysPanel key={project.id} project={project} canManage={canManage} />}
        </main>
      </div>
    </ToastProvider>
  )
}
