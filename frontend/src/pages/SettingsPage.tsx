import { useEffect, useState } from 'react'
import { IS_APP } from '../appTarget'
import { useAuth } from '../auth/AuthContext'
import { useAuthConfig } from '../auth/useAuthConfig'
import AccountPasswordPanel from '../components/AccountPasswordPanel'
import ApiKeysPanel from '../components/ApiKeysPanel'
import McpConnectPanel from '../components/McpConnectPanel'
import DashboardShell from '../components/DashboardShell'
import HeatmapSettingsPanel from '../components/HeatmapSettingsPanel'
import ReplaySettingsPanel from '../components/ReplaySettingsPanel'
import TelegramBotCard from '../components/TelegramBotCard'
import TeamMembersPanel from '../components/TeamMembersPanel'
import { useSearch } from '../router'
import '../dashboard.css'
import { initialSettingsTab, type SettingsTab } from '../utils/deepLink'

export default function SettingsPage() {
  const { reload } = useAuth()
  const authConfig = useAuthConfig()
  const search = useSearch()
  const showAccount = !IS_APP && authConfig.local

  const [keysVersion, setKeysVersion] = useState(0)
  const [activeTab, setActiveTab] = useState<SettingsTab>(() =>
    typeof window === 'undefined' ? 'keys' : initialSettingsTab(window.location.search),
  )

  useEffect(() => {
    setActiveTab(initialSettingsTab(search))
  }, [search])

  const switchTab = (next: SettingsTab) => {
    setActiveTab(next)
    if (typeof window !== 'undefined') {
      const url = new URL(window.location.href)
      url.searchParams.set('tab', next)
      window.history.replaceState(null, '', url.pathname + url.search)
    }
  }

  return (
    <DashboardShell sectionTitle="Settings & Preferences" allowDemo>
      {({ user, activeOrg, project, canManage, isDemo, error }) => {
        const effectiveTab: SettingsTab = !activeOrg
          ? showAccount
            ? 'account'
            : 'keys'
          : activeTab === 'account' && !showAccount
            ? 'keys'
            : activeTab

        return (
          <>
            <div className="dash-page-header">
              <div className="dash-page-header__title-wrap">
                <h1 className="dash-page-header__title">Settings &amp; Management</h1>
                {activeOrg && (
                  <p className="dash-page-header__desc">
                    {activeOrg.name} · <span className="badge badge--role">{activeOrg.role}</span>
                    {project && <> · Project: <strong>{project.name}</strong></>}
                  </p>
                )}
              </div>

              {!isDemo && (activeOrg || showAccount) && (
                <div className="dash-page-tabs" role="tablist" aria-label="Settings navigation">
                  {activeOrg && (
                    <>
                      <button
                        type="button"
                        role="tab"
                        aria-selected={effectiveTab === 'keys'}
                        className={`dash-page-tab${effectiveTab === 'keys' ? ' is-active' : ''}`}
                        onClick={() => switchTab('keys')}
                      >
                        API Keys
                      </button>
                      <button
                        type="button"
                        role="tab"
                        aria-selected={effectiveTab === 'team'}
                        className={`dash-page-tab${effectiveTab === 'team' ? ' is-active' : ''}`}
                        onClick={() => switchTab('team')}
                      >
                        Team &amp; Access
                      </button>
                      <button
                        type="button"
                        role="tab"
                        aria-selected={effectiveTab === 'bot'}
                        className={`dash-page-tab${effectiveTab === 'bot' ? ' is-active' : ''}`}
                        onClick={() => switchTab('bot')}
                      >
                        Telegram Bot
                      </button>
                      <button
                        type="button"
                        role="tab"
                        aria-selected={effectiveTab === 'replay'}
                        className={`dash-page-tab${effectiveTab === 'replay' ? ' is-active' : ''}`}
                        onClick={() => switchTab('replay')}
                      >
                        Session Replay
                      </button>
                    </>
                  )}
                  {showAccount && (
                    <button
                      type="button"
                      role="tab"
                      aria-selected={effectiveTab === 'account'}
                      className={`dash-page-tab${effectiveTab === 'account' ? ' is-active' : ''}`}
                      onClick={() => switchTab('account')}
                    >
                      Account
                    </button>
                  )}
                </div>
              )}
            </div>

            {error && (
              <p className="dash-error" role="alert">
                {error}
              </p>
            )}

            {isDemo ? (
              <section className="dash-card">
                <h2 className="dash-card__title">Sign in to manage live SDK keys &amp; team</h2>
                <p className="dash-card__desc">
                  You are viewing the console in preview mode. Authenticate with Google SSO to create organizations, projects, rotatable API keys, and invite teammates.
                </p>
              </section>
            ) : (
              <>
                {!activeOrg && effectiveTab !== 'account' && (
                  <section className="dash-card">
                    <h2 className="dash-card__title">No Organization Selected</h2>
                    <p className="dash-card__desc">
                      Create or select an organization from the sidebar switcher on the left to get started.
                    </p>
                  </section>
                )}

                {/* Tab: Account (Change / Set Password) */}
                {effectiveTab === 'account' && showAccount && (
                  <AccountPasswordPanel user={user} onUpdated={reload} />
                )}

                {/* Tab 1: Team & Access */}
                {activeOrg && effectiveTab === 'team' && (
                  <TeamMembersPanel
                    org={{ ...activeOrg, created_at: '' }}
                    canManage={canManage}
                    currentUserRole={activeOrg.role}
                  />
                )}

                {/* Tab 2: Telegram Alert Bot */}
                {activeOrg && effectiveTab === 'bot' && (
                  <TelegramBotCard orgId={activeOrg.id} canManage={canManage} />
                )}

                {/* Tab 3: Session Replay Settings */}
                {activeOrg && effectiveTab === 'replay' && (
                  <>
                    {!project && !error ? (
                      <section className="dash-card">
                        <h2 className="dash-card__title">No Project Selected</h2>
                        <p className="dash-card__desc">
                          Replay settings are project-scoped. {canManage ? 'Use the Project switcher in the sidebar to create or select one.' : `Your role (${activeOrg.role}) cannot create projects.`}
                        </p>
                      </section>
                    ) : (
                      project && (
                        <>
                          <ReplaySettingsPanel key={project.id} project={project} canManage={canManage} />
                          <HeatmapSettingsPanel key={`heatmap-${project.id}`} project={project} canManage={canManage} />
                        </>
                      )
                    )}
                  </>
                )}

                {/* Tab 4: API Keys */}
                {activeOrg && effectiveTab === 'keys' && (
                  <>
                    {!project && !error ? (
                      <section className="dash-card">
                        <h2 className="dash-card__title">No Project Selected</h2>
                        <p className="dash-card__desc">
                          API keys authenticate SDKs per project. {canManage ? 'Use the Project switcher in the sidebar to create or select one.' : `Your role (${activeOrg.role}) cannot create projects.`}
                        </p>
                      </section>
                    ) : (
                      project && (
                        <div className="dash-panel-stack">
                          {!canManage && (
                            <p className="dash-note" style={{ marginBottom: 'var(--space-xs)' }}>
                              Your role ({activeOrg.role}) is read-only for projects and SDK keys.
                            </p>
                          )}
                          {!isDemo && activeOrg.role !== 'viewer' && (
                            <McpConnectPanel key={`mcp-${project.id}`} project={project} onCreated={() => setKeysVersion((v) => v + 1)} />
                          )}
                          <ApiKeysPanel key={`${project.id}-${keysVersion}`} project={project} canManage={canManage} canManageMcp={!isDemo && activeOrg.role !== 'viewer'} />
                        </div>
                      )
                    )}
                  </>
                )}
              </>
            )}
          </>
        )
      }}
    </DashboardShell>
  )
}
