import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { orgApi, projectApi, type NavCounts, type OrgMembership, type Platform, type Project, type User } from '../api'
import { useAuth } from '../auth/AuthContext'
import { Link, Redirect, useLocation } from '../router'
import OrgSwitcher from './OrgSwitcher'
import ProjectSelector from './ProjectSelector'
import ThemeToggle from './ThemeToggle'
import { formatNavBadge } from '../utils/navBadge'
import { ToastProvider } from './Toast'

const ORG_KEY = 'artracker.org'
const PROJECT_KEY = 'artracker.project'
const SIDEBAR_COLLAPSED_KEY = 'artracker.sidebar.collapsed'

const DEMO_ORG: OrgMembership = {
  id: 1,
  name: 'Acme Studio',
  slug: 'acme-studio',
  role: 'owner',
}

const DEMO_PROJECT: Project = {
  id: 1,
  org_id: 1,
  name: 'Checkout Web',
  slug: 'checkout-web',
  platform: 'web',
  created_at: '2026-11-04T06:00:00Z',
  replay_screenshot_enabled: true,
}

const DEMO_USER: User = {
  id: 1,
  email: 'demo@artracker.dev',
  name: 'Demo Engineer',
  avatar_url: '',
  orgs: [DEMO_ORG],
}

function readStoredNumber(key: string): number | null {
  try {
    const v = localStorage.getItem(key)
    return v ? Number(v) : null
  } catch {
    return null
  }
}

function storeNumber(key: string, id: number) {
  try {
    localStorage.setItem(key, String(id))
  } catch {
    // storage unavailable (private mode)
  }
}

export type WorkspaceContext = {
  user: User
  isDemo: boolean
  activeOrg: OrgMembership | null
  projects: Project[]
  project: Project | null
  canManage: boolean
  error: string
  dbHealth: 'checking' | 'up' | 'disabled' | 'down'
  selectProject: (id: number) => void
}

const NAV_COUNTS_REFRESH_MS = 60_000

type NavItem = {
  to: string
  label: string
  icon: string
  /** Which live count (GET /projects/:id/nav-counts) this item shows as its badge. */
  badgeKey?: 'replays' | 'issues'
  badgeTone?: 'alert' | 'neutral'
  demoAllowed?: boolean
}

const NAV_SECTIONS: { heading: string; items: NavItem[] }[] = [
  {
    heading: 'Observability',
    items: [
      {
        to: '/dashboard',
        label: 'Overview',
        icon: 'M4 13h4v7H4v-7Zm6-5h4v12h-4V8Zm6-4h4v16h-4V4Z',
        demoAllowed: true,
      },
      {
        to: '/dashboard/replays',
        label: 'Session Replay',
        icon: 'M5 5.5A1.5 1.5 0 0 1 7.3 4.2l11 6.5a1.5 1.5 0 0 1 0 2.6l-11 6.5A1.5 1.5 0 0 1 5 18.5v-13Z',
        badgeKey: 'replays',
        badgeTone: 'neutral',
        demoAllowed: true,
      },
      {
        to: '/dashboard/logs',
        label: 'Event Log',
        icon: 'M4 6h16M4 12h16M4 18h10',
        demoAllowed: true,
      },
      {
        to: '/dashboard/issues',
        label: 'Issues',
        icon: 'M12 8v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z',
        badgeKey: 'issues',
        badgeTone: 'alert',
        demoAllowed: true,
      },
    ],
  },
  {
    heading: 'Growth & Ops',
    items: [
      {
        to: '/dashboard/campaigns',
        label: 'Marketing Campaigns',
        icon: 'M4 19 9 9l4 5 3-4 4 9H4Z',
        demoAllowed: true,
      },
      {
        to: '/dashboard/funnels',
        label: 'Funnel & Retention',
        icon: 'M3 3v18h18M7 16l4-4 4 4 5-6',
        demoAllowed: true,
      },
      {
        to: '/dashboard/alerts',
        label: 'Custom Alerts',
        icon: 'M12 4a6 6 0 0 0-6 6v3.5L4.5 16h15L18 13.5V10a6 6 0 0 0-6-6Zm-2 14a2 2 0 1 0 4 0',
        demoAllowed: true,
      },
    ],
  },
  {
    heading: 'Workspace',
    items: [
      {
        to: '/dashboard/settings',
        label: 'API Keys & Settings',
        icon: 'M15 7a4 4 0 1 1-3.8 5.2L5 18.4V20h2v-2h2v-2l3.8-3.8A4 4 0 0 1 15 7Z',
        demoAllowed: true,
      },
    ],
  },
]

type Props = {
  sectionTitle: string
  toolbar?: ReactNode
  allowDemo?: boolean
  children: (ws: WorkspaceContext) => ReactNode
}

export default function DashboardShell({ sectionTitle, toolbar, allowDemo = false, children }: Props) {
  const { user: authUser, loading, logout, reload } = useAuth()
  const path = useLocation().replace(/\/+$/, '') || '/'
  const isDemoQuery = typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('demo') === '1'
  const isDemo = allowDemo && !authUser && isDemoQuery

  const [storedOrg, setStoredOrg] = useState<number | null>(() => readStoredNumber(ORG_KEY))
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState<number | null>(() => readStoredNumber(PROJECT_KEY))
  const [error, setError] = useState('')
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1'
    } catch {
      return false
    }
  })
  const [dbHealth, setDbHealth] = useState<'checking' | 'up' | 'disabled' | 'down'>('checking')
  const [navState, setNavState] = useState<{ projectId: number; counts: NavCounts } | null>(null)

  const toggleSidebarCollapsed = () => {
    setSidebarCollapsed((prev) => {
      const next = !prev
      try {
        localStorage.setItem(SIDEBAR_COLLAPSED_KEY, next ? '1' : '0')
      } catch {
        // storage unavailable
      }
      return next
    })
  }

  const effectiveUser = authUser ?? (isDemo ? DEMO_USER : null)
  const orgs = effectiveUser?.orgs ?? []
  const activeOrg = orgs.find((o) => o.id === storedOrg) ?? orgs[0] ?? null
  const activeOrgId = activeOrg?.id ?? null
  const canManage = !isDemo && (activeOrg?.role === 'owner' || activeOrg?.role === 'admin')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const res = await fetch('/healthz')
        if (!res.ok) {
          if (!cancelled) setDbHealth('down')
          return
        }
        const body = (await res.json()) as { db?: string }
        if (!cancelled) setDbHealth(body.db === 'up' ? 'up' : 'disabled')
      } catch {
        if (!cancelled) setDbHealth('down')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const loadProjects = useCallback(
    async (orgId: number) => {
      if (isDemo) {
        setProjects([DEMO_PROJECT])
        setProjectId(DEMO_PROJECT.id)
        return
      }
      try {
        const items = (await orgApi.listProjects(orgId)).items
        setProjects(items)
        setProjectId((cur) => {
          const next = items.some((p) => p.id === cur) ? cur : (items[0]?.id ?? null)
          if (next !== null) storeNumber(PROJECT_KEY, next)
          return next
        })
        setError('')
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load projects')
      }
    },
    [isDemo],
  )

  useEffect(() => {
    if (activeOrgId !== null) void loadProjects(activeOrgId)
  }, [activeOrgId, loadProjects])

  // Live sidebar badges: refreshed every minute while the tab is visible, and when it becomes visible again.
  const countsProjectId = isDemo ? null : projectId
  useEffect(() => {
    if (countsProjectId === null) return
    let cancelled = false
    const load = async () => {
      if (document.hidden) return
      try {
        const next = await projectApi.navCounts(countsProjectId)
        if (!cancelled) setNavState({ projectId: countsProjectId, counts: next })
      } catch {
        // keep the last known counts; a badge is never worth an error banner
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), NAV_COUNTS_REFRESH_MS)
    document.addEventListener('visibilitychange', load)
    return () => {
      cancelled = true
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', load)
    }
  }, [countsProjectId])

  if (loading && !isDemoQuery) return null
  if (!effectiveUser) return <Redirect to="/login" />

  const effectiveProjects = isDemo ? [DEMO_PROJECT] : projects
  const project = activeOrg
    ? (effectiveProjects.find((p) => p.id === projectId) ?? effectiveProjects[0] ?? null)
    : null

  const selectOrg = (id: number) => {
    storeNumber(ORG_KEY, id)
    setStoredOrg(id)
  }

  const selectProject = (id: number) => {
    storeNumber(PROJECT_KEY, id)
    setProjectId(id)
  }

  const createOrg = async (name: string) => {
    const org = await orgApi.create(name)
    selectOrg(org.id)
    await reload()
  }

  const createProject = async (input: { name: string; platform: Platform }) => {
    if (!activeOrg) return
    const p = await orgApi.createProject(activeOrg.id, input)
    selectProject(p.id)
    await loadProjects(activeOrg.id)
  }

  const userInitial = (effectiveUser.name || effectiveUser.email || 'A').trim().charAt(0).toUpperCase()
  const withDemo = (to: string) => (isDemo ? `${to}?demo=1` : to)
  const badgeFor = (item: NavItem) => {
    // Counts of a previously selected project are never shown for the current one.
    const navCounts = navState && navState.projectId === countsProjectId ? navState.counts : null
    if (!navCounts || !item.badgeKey) return undefined
    return formatNavBadge(item.badgeKey === 'replays' ? navCounts.frustrated_replays_24h : navCounts.open_issues)
  }

  return (
    <ToastProvider>
      <div className={`console-layout${sidebarCollapsed ? ' is-collapsed' : ''}`}>
        {sidebarOpen && (
          <div
            className="console-backdrop"
            aria-hidden="true"
            onClick={() => setSidebarOpen(false)}
          />
        )}

        {/* Left Sidebar */}
        <aside
          className={`console-sidebar${sidebarOpen ? ' is-open' : ''}${sidebarCollapsed ? ' is-collapsed' : ''}`}
          aria-label="Workspace navigation"
        >
          <div className="console-sidebar__brand">
            <Link to="/" className="dash-brand" aria-label="ARTracker home" title="ARTracker">
              <span className="sso-logo-badge" aria-hidden="true">
                <svg className="sso-logo-badge__svg" viewBox="0 0 24 24" fill="none">
                  <path d="M4 19L12 4L20 19M7.5 14H16.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="square" />
                </svg>
              </span>
              <span className="dash-brand__name">ARTracker</span>
            </Link>
            <div className="console-sidebar__brand-actions">
              <span className="console-sidebar__ver">Console</span>
              <button
                type="button"
                className="dash-icon-btn console-sidebar__collapse-btn"
                onClick={toggleSidebarCollapsed}
                aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
                title={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <rect x="3" y="4" width="18" height="16" rx="2" />
                  <path d="M9 4v16" />
                  {sidebarCollapsed ? <path d="m13 9 3 3-3 3" /> : <path d="m15 9-3 3 3 3" />}
                </svg>
              </button>
            </div>
          </div>

          {/* Organization & Project Switchers inside Sidebar */}
          <div className="console-sidebar__switchers">
            {isDemo ? (
              <div className="console-demo-scope">
                <div className="console-demo-scope__row">
                  <span className="picker__caption">Organization</span>
                  <span className="picker__value">{DEMO_ORG.name}</span>
                </div>
                <div className="console-demo-scope__row">
                  <span className="picker__caption">Project</span>
                  <span className="picker__value">
                    {DEMO_PROJECT.name} · <span className="dash-code">{DEMO_PROJECT.platform}</span>
                  </span>
                </div>
              </div>
            ) : (
              <>
                <OrgSwitcher orgs={orgs} activeId={activeOrgId} onChange={selectOrg} onCreate={createOrg} />
                {activeOrg && (
                  <ProjectSelector
                    projects={effectiveProjects}
                    activeId={project?.id ?? null}
                    onChange={selectProject}
                    canManage={canManage}
                    onCreate={createProject}
                  />
                )}
              </>
            )}
          </div>

          {/* Grouped Sidebar Navigation */}
          <nav className="console-nav" aria-label="Console modules">
            {NAV_SECTIONS.map((sec) => {
              const visibleItems = sec.items.filter((it) => !isDemo || it.demoAllowed)
              if (visibleItems.length === 0) return null
              return (
                <div key={sec.heading} className="console-nav__group">
                  <span className="console-nav__heading">{sec.heading}</span>
                  <div className="console-nav__list">
                    {visibleItems.map((item) => {
                      const active = path === item.to
                      return (
                        <Link
                          key={item.to}
                          to={withDemo(item.to)}
                          onClick={() => setSidebarOpen(false)}
                          className={`console-nav__item${active ? ' is-active' : ''}`}
                          aria-current={active ? 'page' : undefined}
                          title={sidebarCollapsed ? item.label : undefined}
                        >
                          <span className="console-nav__item-left">
                            <svg
                              className="console-nav__icon"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="1.8"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              aria-hidden="true"
                            >
                              <path d={item.icon} />
                            </svg>
                            <span>{item.label}</span>
                          </span>
                          {badgeFor(item) && (
                            <span
                              className={`console-nav__badge${
                                item.badgeTone === 'alert' ? ' console-nav__badge--alert' : ''
                              }`}
                            >
                              {badgeFor(item)}
                            </span>
                          )}
                        </Link>
                      )
                    })}
                  </div>
                </div>
              )
            })}
          </nav>

          {/* Bottom Account */}
          <div className="console-sidebar__foot">
            <div className="console-account">
              <span className="console-account__avatar" aria-hidden="true" title={effectiveUser.email}>
                {userInitial}
              </span>
              <div className="console-account__meta">
                <strong className="console-account__name">
                  {effectiveUser.name || effectiveUser.email.split('@')[0]}
                </strong>
                <span className="console-account__email">{effectiveUser.email}</span>
              </div>
              <ThemeToggle showLabel={false} />
              {isDemo ? (
                <Link to="/login" className="dash-btn">
                  Sign in
                </Link>
              ) : (
                <button
                  type="button"
                  className="dash-icon-btn"
                  title="Log out"
                  aria-label="Log out"
                  onClick={() => void logout()}
                >
                  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
                  </svg>
                </button>
              )}
            </div>
          </div>
        </aside>

        {/* Right Workspace Column */}
        <div className="console-workspace">
          <header className="console-topbar">
            <div className="console-topbar__left">
              <button
                type="button"
                className="dash-icon-btn console-menu-btn"
                aria-label="Open navigation menu"
                onClick={() => setSidebarOpen(true)}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                  <path d="M4 7h16M4 12h16M4 17h16" />
                </svg>
              </button>

              <div className="console-crumbs">
                {activeOrg && (
                  <>
                    <span className="console-crumbs__muted">{activeOrg.name}</span>
                    <span className="console-crumbs__sep" aria-hidden="true">/</span>
                  </>
                )}
                {project && (
                  <>
                    <span className="console-crumbs__muted">{project.name}</span>
                    <span className="console-crumbs__sep" aria-hidden="true">/</span>
                  </>
                )}
                <strong className="console-crumbs__current">{sectionTitle}</strong>
                {isDemo && <span className="badge badge--preview">Preview Data</span>}
              </div>
            </div>

            <div className="console-topbar__right">
              {toolbar}
              <ThemeToggle />
            </div>
          </header>

          <main className="console-content">
            {children({
              user: effectiveUser,
              isDemo,
              activeOrg,
              projects: effectiveProjects,
              project,
              canManage,
              error,
              dbHealth,
              selectProject,
            })}
          </main>
        </div>
      </div>
    </ToastProvider>
  )
}
