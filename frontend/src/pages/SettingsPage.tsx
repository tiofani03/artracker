import { useState } from 'react'
import ApiKeysPanel from '../components/ApiKeysPanel'
import DashboardShell from '../components/DashboardShell'
import ReplaySettingsPanel from '../components/ReplaySettingsPanel'
import TeamMembersPanel from '../components/TeamMembersPanel'
import '../dashboard.css'

export default function SettingsPage() {
  const [activeTab, setActiveTab] = useState<'keys' | 'team' | 'replay'>('keys')

  return (
    <DashboardShell sectionTitle="API Keys & Settings" allowDemo>
      {({ activeOrg, project, canManage, isDemo, error }) => (
        <>
          <div className="dash-title">
            <div>
              <h1>Settings &amp; Management</h1>
              {activeOrg && (
                <p>
                  {activeOrg.name} · <span className="badge badge--role">{activeOrg.role}</span>
                </p>
              )}
            </div>

            {activeOrg && !isDemo && (
              <div className="dash-seg" role="group" aria-label="Settings navigation">
                <button
                  type="button"
                  className={`dash-seg__btn${activeTab === 'keys' ? ' is-active' : ''}`}
                  aria-pressed={activeTab === 'keys'}
                  onClick={() => setActiveTab('keys')}
                >
                  API Keys
                </button>
                <button
                  type="button"
                  className={`dash-seg__btn${activeTab === 'replay' ? ' is-active' : ''}`}
                  aria-pressed={activeTab === 'replay'}
                  onClick={() => setActiveTab('replay')}
                >
                  Replay
                </button>
                <button
                  type="button"
                  className={`dash-seg__btn${activeTab === 'team' ? ' is-active' : ''}`}
                  aria-pressed={activeTab === 'team'}
                  onClick={() => setActiveTab('team')}
                >
                  Team &amp; Members
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
                <p className="dash-empty dash-card">
                  Create an organization from the sidebar switcher on the left to get started.
                </p>
              )}

              {activeOrg && activeTab === 'team' && (
                <TeamMembersPanel
                  org={{ ...activeOrg, created_at: '' }}
                  canManage={canManage}
                  currentUserRole={activeOrg.role}
                />
              )}

              {activeOrg && activeTab === 'replay' && (
                <>
                  {!project && !error && (
                    <p className="dash-empty dash-card">
                      No projects yet.{' '}
                      {canManage ? 'Use the Project switcher in the sidebar to create one.' : `Your role (${activeOrg.role}) cannot create projects.`}
                    </p>
                  )}
                  {project && <ReplaySettingsPanel key={project.id} project={project} canManage={canManage} />}
                </>
              )}

              {activeOrg && activeTab === 'keys' && (
                <>
                  {!project && !error && (
                    <p className="dash-empty dash-card">
                      No projects yet.{' '}
                      {canManage ? 'Use the Project switcher in the sidebar to create one.' : `Your role (${activeOrg.role}) cannot create projects.`}
                    </p>
                  )}

                  {!canManage && project && (
                    <p className="dash-note">Your role ({activeOrg.role}) is read-only for projects and keys.</p>
                  )}

                  {project && <ApiKeysPanel key={project.id} project={project} canManage={canManage} />}
                </>
              )}
            </>
          )}
        </>
      )}
    </DashboardShell>
  )
}
