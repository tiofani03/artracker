import { useState } from 'react'
import ApiKeysPanel from '../components/ApiKeysPanel'
import DashboardShell from '../components/DashboardShell'
import ReplaySettingsPanel from '../components/ReplaySettingsPanel'
import TelegramBotCard from '../components/TelegramBotCard'
import TeamMembersPanel from '../components/TeamMembersPanel'
import '../dashboard.css'
import { initialSettingsTab, type SettingsTab } from '../utils/deepLink'

export default function SettingsPage() {
  const [activeTab, setActiveTab] = useState<SettingsTab>(() =>
    typeof window === 'undefined' ? 'keys' : initialSettingsTab(window.location.search),
  )

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
      {({ activeOrg, project, canManage, isDemo, error }) => (
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

            {activeOrg && !isDemo && (
              <div className="dash-page-tabs" role="tablist" aria-label="Settings navigation">
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === 'keys'}
                  className={`dash-page-tab${activeTab === 'keys' ? ' is-active' : ''}`}
                  onClick={() => switchTab('keys')}
                >
                  API Keys
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === 'team'}
                  className={`dash-page-tab${activeTab === 'team' ? ' is-active' : ''}`}
                  onClick={() => switchTab('team')}
                >
                  Team &amp; Access
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === 'bot'}
                  className={`dash-page-tab${activeTab === 'bot' ? ' is-active' : ''}`}
                  onClick={() => switchTab('bot')}
                >
                  Telegram Bot
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === 'replay'}
                  className={`dash-page-tab${activeTab === 'replay' ? ' is-active' : ''}`}
                  onClick={() => switchTab('replay')}
                >
                  Session Replay
                </button>
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
              {!activeOrg && (
                <section className="dash-card">
                  <h2 className="dash-card__title">No Organization Selected</h2>
                  <p className="dash-card__desc">
                    Create or select an organization from the sidebar switcher on the left to get started.
                  </p>
                </section>
              )}

              {/* Tab 1: Team & Access */}
              {activeOrg && activeTab === 'team' && (
                <TeamMembersPanel
                  org={{ ...activeOrg, created_at: '' }}
                  canManage={canManage}
                  currentUserRole={activeOrg.role}
                />
              )}

              {/* Tab 2: Telegram Alert Bot */}
              {activeOrg && activeTab === 'bot' && (
                <TelegramBotCard orgId={activeOrg.id} canManage={canManage} />
              )}

              {/* Tab 3: Session Replay Settings */}
              {activeOrg && activeTab === 'replay' && (
                <>
                  {!project && !error ? (
                    <section className="dash-card">
                      <h2 className="dash-card__title">No Project Selected</h2>
                      <p className="dash-card__desc">
                        Replay settings are project-scoped. {canManage ? 'Use the Project switcher in the sidebar to create or select one.' : `Your role (${activeOrg.role}) cannot create projects.`}
                      </p>
                    </section>
                  ) : (
                    project && <ReplaySettingsPanel key={project.id} project={project} canManage={canManage} />
                  )}
                </>
              )}

              {/* Tab 4: API Keys */}
              {activeOrg && activeTab === 'keys' && (
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
                      <>
                        {!canManage && (
                          <p className="dash-note" style={{ marginBottom: 'var(--space-xs)' }}>
                            Your role ({activeOrg.role}) is read-only for projects and keys.
                          </p>
                        )}
                        <ApiKeysPanel key={project.id} project={project} canManage={canManage} />
                      </>
                    )
                  )}
                </>
              )}
            </>
          )}
        </>
      )}
    </DashboardShell>
  )
}
