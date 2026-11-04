import ApiKeysPanel from '../components/ApiKeysPanel'
import DashboardShell from '../components/DashboardShell'
import '../dashboard.css'

export default function SettingsPage() {
  return (
    <DashboardShell sectionTitle="API Keys & Settings" allowDemo>
      {({ activeOrg, project, canManage, isDemo, error }) => (
        <>
          <div className="dash-title">
            <h1>API Keys &amp; Settings</h1>
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

          {isDemo ? (
            <section className="dash-card">
              <h2 className="dash-card__title">Sign in to manage live SDK keys</h2>
              <p className="dash-card__desc">
                You are viewing the console in preview mode. Authenticate with Google SSO to create organizations, projects, and rotatable API keys.
              </p>
            </section>
          ) : (
            <>
              {!activeOrg && (
                <p className="dash-empty dash-card">
                  Create an organization from the sidebar switcher on the left to get started.
                </p>
              )}

              {activeOrg && !project && !error && (
                <p className="dash-empty dash-card">
                  No projects yet.{' '}
                  {canManage ? 'Use the Project switcher in the sidebar to create one.' : `Your role (${activeOrg.role}) cannot create projects.`}
                </p>
              )}

              {activeOrg && !canManage && project && (
                <p className="dash-note">Your role ({activeOrg.role}) is read-only for projects and keys.</p>
              )}

              {project && <ApiKeysPanel key={project.id} project={project} canManage={canManage} />}
            </>
          )}
        </>
      )}
    </DashboardShell>
  )
}
