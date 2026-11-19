import { useEffect, useState } from 'react'
import { appStartApi, type AppStartDetail } from '../api'
import { formatMs } from '../appstart/format'
import DashboardShell from '../components/DashboardShell'
import Flamegraph from '../components/appstart/Flamegraph'
import PhaseBar from '../components/appstart/PhaseBar'
import { Link, useLocation } from '../router'

const errMsg = (e: unknown) => (e instanceof Error ? e.message : 'Request failed')

function ProfileBanner({ detail }: { detail: AppStartDetail }) {
  const profile = detail.profile
  if (!profile) {
    return (
      <p className="hm-note">
        {detail.profiled
          ? 'Profile expired (profiles are kept 30 days) or was not received.'
          : 'This start was not profiled.'}
      </p>
    )
  }
  switch (profile.status) {
    case 'pending':
      return <p className="hm-note">Profile is being processed — it appears within a minute.</p>
    case 'missing_symbols':
      return (
        <p className="hm-note hm-note--warn">
          Some frames are not symbolicated. Upload the ProGuard/R8 mapping or dSYM for this version.{' '}
          <Link to="/docs#release-artifacts">How to upload</Link>
        </p>
      )
    case 'failed':
      return <p className="hm-note hm-note--warn">This profile could not be read.</p>
    default:
      return null
  }
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="as-fact">
      <dt>{label}</dt>
      <dd>{value || '—'}</dd>
    </div>
  )
}

function DetailView({ projectId, startId }: { projectId: number; startId: string }) {
  const [res, setRes] = useState<{ detail: AppStartDetail | null; error: string } | null>(null)
  useEffect(() => {
    let cancelled = false
    appStartApi
      .get(projectId, startId)
      .then((detail) => !cancelled && setRes({ detail, error: '' }))
      .catch((e: unknown) => !cancelled && setRes({ detail: null, error: errMsg(e) }))
    return () => {
      cancelled = true
    }
  }, [projectId, startId])

  const detail = res?.detail
  return (
    <>
      <div className="dash-page-header issue-header">
        <Link to="/dashboard/app-start" className="issue-header__back">
          ← App Start
        </Link>
      </div>
      {!res ? (
        <p className="ppl-muted ppl-pad">Loading…</p>
      ) : !detail ? (
        <section className="dash-card fb-card ppl-pad">
          <p className="dash-empty">Couldn't load this start: {res.error}</p>
        </section>
      ) : (
        <div className="as-page">
          <section className="dash-card fb-card">
            <div className="dash-card__head">
              <div>
                <h2 className="dash-card__title">
                  {detail.type.charAt(0).toUpperCase() + detail.type.slice(1)} start ·{' '}
                  <span className="as-duration">{formatMs(detail.duration_ms)}</span>
                </h2>
                <p className="dash-card__desc">{new Date(detail.started_at).toLocaleString()}</p>
              </div>
              {detail.session_id && (
                <Link to={`/dashboard/replays?session=${encodeURIComponent(detail.session_id)}`} className="dash-btn">
                  Open session replay
                </Link>
              )}
            </div>
            <PhaseBar phases={detail.phases} total={detail.duration_ms} />
          </section>

          <section className="dash-card fb-card">
            <h2 className="dash-card__title">Details</h2>
            <dl className="as-facts">
              <Fact label="Type" value={detail.type} />
              <Fact label="Duration" value={formatMs(detail.duration_ms)} />
              <Fact label="Version" value={detail.build ? `${detail.app_version} (${detail.build})` : detail.app_version} />
              <Fact label="Device" value={detail.device_model} />
              <Fact label="OS" value={`${detail.platform} ${detail.os_version}`.trim()} />
              <Fact label="RAM" value={detail.ram_mb ? `${(detail.ram_mb / 1024).toFixed(1)} GB` : ''} />
              <Fact label="First screen" value={detail.first_screen} />
              <Fact label="Trampoline" value={detail.trampoline ? 'Yes' : 'No'} />
            </dl>
          </section>

          <section className="dash-card fb-card">
            <h2 className="dash-card__title">Startup flamegraph</h2>
            <ProfileBanner detail={detail} />
            {detail.profile?.data && <Flamegraph profile={detail.profile.data} />}
          </section>
        </div>
      )}
    </>
  )
}

export default function AppStartDetailPage() {
  const path = useLocation().replace(/\/+$/, '') || '/'
  const raw = path.split('/').filter(Boolean).pop() ?? ''
  let startId = raw
  try {
    startId = decodeURIComponent(raw)
  } catch {
    // keep the raw segment
  }

  return (
    <DashboardShell sectionTitle="App Start">
      {({ project }) => {
        if (!project) return <p className="dash-empty dash-card ppl-pad">Select a project to view this start.</p>
        if (!startId || startId === 'app-start') {
          return (
            <section className="dash-card ppl-pad">
              <p className="dash-empty">Invalid start ID.</p>
              <Link to="/dashboard/app-start" className="dash-btn">
                Back to App Start
              </Link>
            </section>
          )
        }
        return <DetailView key={`${project.id}:${startId}`} projectId={project.id} startId={startId} />
      }}
    </DashboardShell>
  )
}
