import { useCallback, useEffect, useRef, useState } from 'react'
import {
  alertApi,
  type AlertChannel,
  type AlertIncident,
  type AlertLinkCode,
  type AlertRule,
} from '../../api'
import { Link } from '../../router'
import {
  SPIKE_WINDOWS,
  buildRulePatch,
  canShowTest,
  channelActivityLabel,
  detectLinked,
  draftFromRule,
  formatCountdown,
  incidentHeadline,
  normalizeChannelsResponse,
  notifyLabel,
  channelsBanner,
  type NormalizedChannels,
  ruleDescription,
  ruleTitle,
  showLinkFlow,
  timeAgo,
  validateChat,
  validateLabel,
  validateRulePatch,
  type RuleDraft,
} from './alertFormat'

const POLL_MS = 3000

function errMsg(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

type Props = { projectId: number; canManage: boolean }
type ViewProps = Props & { orgId: number }

export default function AlertsView({ projectId, orgId, canManage }: ViewProps) {
  // keyed by project (and org, whose bot decides the mode) so each card starts from fresh state
  // and its timers are torn down on switch
  return (
    <div className="dash-panel-stack">
      <ChannelsCard key={`c${orgId}-${projectId}`} projectId={projectId} canManage={canManage} />
      <RulesCard key={`r${projectId}`} projectId={projectId} canManage={canManage} />
      <IncidentsCard key={`i${projectId}`} projectId={projectId} />
    </div>
  )
}

type LinkPhase = 'active' | 'connected' | 'expired'
// before: the channels when the code was created, to tell when the code was used
type LinkState = { code: AlertLinkCode; before: AlertChannel[]; phase: LinkPhase }
const NO_CHANNELS: AlertChannel[] = []

function ChannelsCard({ projectId, canManage }: Props) {
  const [data, setData] = useState<NormalizedChannels | null>(null)
  const [error, setError] = useState('')
  const [link, setLink] = useState<LinkState | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [creating, setCreating] = useState(false)
  const [testResult, setTestResult] = useState<Record<number, string>>({})
  const [pending, setPending] = useState<Set<number>>(() => new Set())
  const [chat, setChat] = useState('')
  const [label, setLabel] = useState('')
  const [adding, setAdding] = useState(false)
  const [addWarning, setAddWarning] = useState('')
  const mounted = useRef(true)
  const pendingRef = useRef<Set<number>>(new Set())

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const load = useCallback(async () => {
    try {
      const res = await alertApi.channels(projectId)
      if (!mounted.current) return
      setData(normalizeChannelsResponse(res))
      setError('')
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to load channels'))
    }
  }, [projectId])

  useEffect(() => {
    let cancelled = false
    alertApi
      .channels(projectId)
      .then((res) => {
        if (cancelled) return
        setData(normalizeChannelsResponse(res))
        setNow(Date.now())
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err, 'Failed to load channels'))
      })
    return () => {
      cancelled = true
    }
  }, [projectId])

  const linkActive = link?.phase === 'active'
  const expiresAt = link ? new Date(link.code.expires_at).getTime() : 0
  const before = link?.before ?? NO_CHANNELS

  // Countdown and poll run only while a code is active; both are cleared on unmount, project
  // change (remount), and as soon as the phase leaves 'active'.
  useEffect(() => {
    if (!linkActive) return
    let cancelled = false
    const tick = setInterval(() => {
      if (cancelled) return
      const t = Date.now()
      setNow(t)
      if (t >= expiresAt) setLink((l) => (l && l.phase === 'active' ? { ...l, phase: 'expired' } : l))
    }, 1000)
    const poll = setInterval(async () => {
      try {
        const res = await alertApi.channels(projectId)
        if (cancelled) return
        const norm = normalizeChannelsResponse(res)
        setData(norm)
        if (detectLinked(before, norm.items)) {
          setLink((l) => (l && l.phase === 'active' ? { ...l, phase: 'connected' } : l))
        }
      } catch {
        // transient poll failure; the next tick retries
      }
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(tick)
      clearInterval(poll)
    }
  }, [linkActive, expiresAt, before, projectId])

  const connect = async () => {
    setCreating(true)
    setError('')
    try {
      const code = await alertApi.createLinkCode(projectId)
      if (!mounted.current) return
      setNow(Date.now())
      setLink({ code, before: data?.items ?? NO_CHANNELS, phase: 'active' })
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to create link code'))
    } finally {
      if (mounted.current) setCreating(false)
    }
  }

  // Guards double clicks: a channel's buttons are disabled (and ignored) while one of its requests runs.
  const guarded = async (id: number, fn: () => Promise<void>) => {
    if (pendingRef.current.has(id)) return
    pendingRef.current.add(id)
    setPending(new Set(pendingRef.current))
    try {
      await fn()
    } finally {
      pendingRef.current.delete(id)
      if (mounted.current) setPending(new Set(pendingRef.current))
    }
  }

  const addChat = async () => {
    if (adding || validateChat(chat) !== null || validateLabel(label) !== null) return
    setAdding(true)
    setError('')
    setAddWarning('')
    try {
      const res = await alertApi.createChannel(projectId, { chat, label })
      if (!mounted.current) return
      setChat('')
      setLabel('')
      setAddWarning(res.warning ?? '')
      await load()
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to add the chat'))
    } finally {
      if (mounted.current) setAdding(false)
    }
  }

  const test = (ch: AlertChannel) => guarded(ch.id, async () => {
    setTestResult((r) => ({ ...r, [ch.id]: 'Sending…' }))
    let result = 'Sent ✓'
    try {
      await alertApi.testChannel(projectId, ch.id)
    } catch (err) {
      result = errMsg(err, 'Test failed')
    }
    if (mounted.current) setTestResult((r) => ({ ...r, [ch.id]: result }))
  })

  const toggle = (ch: AlertChannel) => guarded(ch.id, async () => {
    try {
      await alertApi.setChannelEnabled(projectId, ch.id, !ch.enabled)
      await load()
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to update channel'))
    }
  })

  const remove = (ch: AlertChannel) => guarded(ch.id, async () => {
    if (!window.confirm(`Remove ${ch.title}? Alerts will no longer be sent there.`)) return
    try {
      await alertApi.deleteChannel(projectId, ch.id)
      await load()
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to remove channel'))
    }
  })

  const banner = data ? channelsBanner(data, canManage) : null
  const chatProblem = chat === '' ? null : validateChat(chat)
  const labelProblem = label === '' ? null : validateLabel(label)
  const activity = (ch: AlertChannel) => (data ? channelActivityLabel(ch, data.mode) : null)

  return (
    <section className="dash-card dash-card--compact" aria-labelledby="alert-channels-title">
      <div className="dash-card__head">
        <div>
          <h2 id="alert-channels-title" className="dash-card__title">
            Telegram channels
          </h2>
          <p className="dash-card__desc">Chats that receive alerts for this project.</p>
        </div>
        {canManage && data && showLinkFlow(data.mode) && data.telegram.configured && (
          <button type="button" className="dash-btn" onClick={() => void connect()} disabled={creating || linkActive}>
            Connect Telegram
          </button>
        )}
      </div>

      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {data === null ? (
        !error && <p className="dash-empty">Loading…</p>
      ) : showLinkFlow(data.mode) && !data.telegram.configured ? (
        <p className="dash-empty">Telegram is not configured on this server. Set TELEGRAM_BOT_TOKEN.</p>
      ) : (
        <>
          {banner && (
            <p className="dash-note" role="status">
              {banner}
              {canManage && (
                <>
                  {' '}
                  <Link to="/dashboard/settings">Open Settings</Link>
                </>
              )}
            </p>
          )}

          {canManage && !showLinkFlow(data.mode) && (
            <div className="dash-form">
              <p className="dash-list__sub" id="alert-add-chat-help">
                Add your bot to the chat or group and send a message there, then enter the chat id (or @channel_username)
                below.
              </p>
              <div className="dash-list__top">
                <input
                  type="text"
                  className="dash-input"
                  aria-label="Chat id or @channel_username"
                  aria-describedby={chatProblem ? 'alert-add-chat-error' : 'alert-add-chat-help'}
                  placeholder="-1001234567890"
                  value={chat}
                  disabled={adding}
                  onChange={(e) => setChat(e.target.value)}
                />
                <input
                  type="text"
                  className="dash-input"
                  aria-label="Chat label"
                  aria-describedby={labelProblem ? 'alert-add-label-error' : undefined}
                  placeholder="Label, e.g. On-call"
                  value={label}
                  disabled={adding}
                  onChange={(e) => setLabel(e.target.value)}
                />
                <button
                  type="button"
                  className="dash-btn"
                  disabled={adding || validateChat(chat) !== null || validateLabel(label) !== null}
                  onClick={() => void addChat()}
                >
                  {adding ? 'Adding…' : 'Add chat'}
                </button>
              </div>
              {chatProblem && (
                <p id="alert-add-chat-error" className="dash-error">
                  {chatProblem}
                </p>
              )}
              {labelProblem && (
                <p id="alert-add-label-error" className="dash-error">
                  {labelProblem}
                </p>
              )}
              <p className="dash-list__sub" aria-live="polite">
                {addWarning}
              </p>
            </div>
          )}

          {link && showLinkFlow(data.mode) && (
            <div className="dash-list__row" aria-live="polite">
              <div className="dash-list__main">
                {link.phase === 'active' && (
                  <>
                    <div className="dash-list__top">
                      <a href={link.code.group_link} target="_blank" rel="noopener noreferrer" className="dash-btn">
                        Add to a group
                      </a>
                      <a href={link.code.private_link} target="_blank" rel="noopener noreferrer" className="dash-btn">
                        Private chat
                      </a>
                      <span className="dash-code">Expires in {formatCountdown(expiresAt - now)}</span>
                    </div>
                    <p className="dash-list__sub">Open one of the links in Telegram and press Start.</p>
                  </>
                )}
                {link.phase === 'connected' && <p className="dash-list__title">Connected ✓</p>}
                {link.phase === 'expired' && <p className="dash-list__title">Link expired — generate a new one</p>}
              </div>
            </div>
          )}

          {data.items.length === 0 ? (
            <p className="dash-empty">No channel connected.</p>
          ) : (
            <div className="dash-list">
              {data.items.map((ch) => (
                <div key={ch.id} className="dash-list__row">
                  <div className="dash-list__main">
                    <div className="dash-list__top">
                      <span className={`badge ${ch.enabled ? 'badge--active' : 'badge--revoked'}`}>
                        {ch.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                      <span className="dash-code">{ch.chat_type}</span>
                      {activity(ch) && <span className="dash-code">{activity(ch)}</span>}
                      {!ch.enabled && ch.disabled_reason && <span className="dash-code">{ch.disabled_reason}</span>}
                      {ch.last_sent_at && <span className="dash-code">Last sent {timeAgo(ch.last_sent_at, new Date(now))}</span>}
                    </div>
                    <p className="dash-list__title">{ch.title}</p>
                    {ch.destination !== '' && <p className="dash-list__sub dash-code">{ch.destination}</p>}
                    {ch.last_error !== '' && <p className="dash-list__sub dash-code">{ch.last_error}</p>}
                    <p className="dash-list__sub" aria-live="polite">
                      {testResult[ch.id] ?? ''}
                    </p>
                  </div>
                  {canManage && (
                    <div className="dash-list__top">
                      {canShowTest(ch) && (
                        <button type="button" className="dash-btn" disabled={pending.has(ch.id)} onClick={() => void test(ch)}>
                          Test
                        </button>
                      )}
                      <button type="button" className="dash-btn" disabled={pending.has(ch.id)} onClick={() => void toggle(ch)}>
                        {ch.enabled ? 'Disable' : 'Enable'}
                      </button>
                      <button type="button" className="dash-btn" disabled={pending.has(ch.id)} onClick={() => void remove(ch)}>
                        Remove
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </section>
  )
}

function RulesCard({ projectId, canManage }: Props) {
  const [rules, setRules] = useState<AlertRule[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    alertApi
      .rules(projectId)
      .then((res) => {
        if (!cancelled) setRules(res.items)
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err, 'Failed to load rules'))
      })
    return () => {
      cancelled = true
    }
  }, [projectId])

  const replace = (updated: AlertRule) =>
    setRules((cur) => (cur ? cur.map((r) => (r.kind === updated.kind ? updated : r)) : cur))

  return (
    <section className="dash-card dash-card--compact" aria-labelledby="alert-rules-title">
      <div className="dash-card__head">
        <div>
          <h2 id="alert-rules-title" className="dash-card__title">
            Rules
          </h2>
          <p className="dash-card__desc">When an alert is sent to your Telegram chats.</p>
        </div>
      </div>
      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}
      {rules === null ? (
        !error && <p className="dash-empty">Loading…</p>
      ) : (
        <div className="dash-list">
          {rules.map((r) => (
            <RuleRow key={r.kind} projectId={projectId} rule={r} canManage={canManage} onSaved={replace} />
          ))}
        </div>
      )}
    </section>
  )
}

function RuleRow({
  projectId,
  rule,
  canManage,
  onSaved,
}: {
  projectId: number
  rule: AlertRule
  canManage: boolean
  onSaved: (rule: AlertRule) => void
}) {
  const [draft, setDraft] = useState<RuleDraft>(() => draftFromRule(rule))
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const patch = buildRulePatch(rule, draft)
  const dirty = Object.keys(patch).length > 0
  const problem = dirty ? validateRulePatch(rule.kind, patch) : null
  const set = (p: Partial<RuleDraft>) => setDraft((d) => ({ ...d, ...p }))
  const title = ruleTitle(rule.kind)
  const spike = rule.kind === 'issue_spike'
  const envHelpId = `alert-rule-env-help-${rule.kind}`

  const save = async () => {
    setSaving(true)
    setSaveError('')
    try {
      const updated = await alertApi.patchRule(projectId, rule.kind, patch)
      if (!mounted.current) return
      setDraft(draftFromRule(updated))
      onSaved(updated)
    } catch (err) {
      if (mounted.current) setSaveError(errMsg(err, 'Failed to save rule'))
    } finally {
      if (mounted.current) setSaving(false)
    }
  }

  return (
    <div className="dash-list__row">
      <div className="dash-list__main">
        <div className="dash-list__top">
          <label className="dash-list__title">
            <input
              type="checkbox"
              aria-label={`${title} enabled`}
              checked={draft.enabled}
              disabled={!canManage || saving}
              onChange={(e) => set({ enabled: e.target.checked })}
            />{' '}
            {title}
          </label>
        </div>
        <p className="dash-list__sub">{ruleDescription(rule)}</p>
        <div className="dash-list__top">
          <input
            type="text"
            className="dash-input"
            aria-label={`${title} environment`}
            aria-describedby={envHelpId}
            placeholder="all environments"
            maxLength={128}
            value={draft.environment}
            disabled={!canManage || saving}
            onChange={(e) => set({ environment: e.target.value })}
          />
          {spike && (
            <>
              <input
                type="number"
                className="dash-input"
                aria-label="Multiplier"
                title="Multiplier"
                step={0.5}
                value={draft.multiplier}
                disabled={!canManage || saving}
                onChange={(e) => set({ multiplier: e.target.value })}
              />
              <input
                type="number"
                className="dash-input"
                aria-label="Minimum events"
                title="Minimum events"
                step={1}
                value={draft.minCount}
                disabled={!canManage || saving}
                onChange={(e) => set({ minCount: e.target.value })}
              />
              <select
                className="dash-input"
                aria-label="Window in minutes"
                title="Window in minutes"
                value={draft.window}
                disabled={!canManage || saving}
                onChange={(e) => set({ window: e.target.value })}
              >
                {SPIKE_WINDOWS.map((w) => (
                  <option key={w} value={String(w)}>
                    {w} min
                  </option>
                ))}
              </select>
              <input
                type="number"
                className="dash-input"
                aria-label="Cooldown minutes"
                title="Cooldown minutes"
                step={1}
                value={draft.cooldown}
                disabled={!canManage || saving}
                onChange={(e) => set({ cooldown: e.target.value })}
              />
            </>
          )}
        </div>
        <p id={envHelpId} className="dash-list__sub">
          Must match the environment your SDK reports (default: production). Leave blank to alert on every environment.
        </p>
        {problem && <p className="dash-error">{problem}</p>}
        {saveError && (
          <p className="dash-error" role="alert">
            {saveError}
          </p>
        )}
      </div>
      {canManage && (
        <button type="button" className="dash-btn" disabled={!dirty || problem !== null || saving} onClick={() => void save()}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      )}
    </div>
  )
}

type StatusFilter = 'firing' | 'resolved' | 'all'
const STATUS_FILTERS: { id: StatusFilter; label: string }[] = [
  { id: 'firing', label: 'Firing' },
  { id: 'resolved', label: 'Resolved' },
  { id: 'all', label: 'All' },
]

function IncidentsCard({ projectId }: { projectId: number }) {
  const [status, setStatus] = useState<StatusFilter>('firing')
  const [items, setItems] = useState<AlertIncident[] | null>(null)
  const [now, setNow] = useState(() => new Date())
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    alertApi
      .incidents(projectId, status)
      .then((res) => {
        if (cancelled) return
        setItems(res.items)
        setNow(new Date())
        setError('')
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err, 'Failed to load incidents'))
      })
    return () => {
      cancelled = true
    }
  }, [projectId, status])

  const pick = (next: StatusFilter) => {
    if (next === status) return
    setItems(null)
    setError('')
    setStatus(next)
  }

  return (
    <section className="dash-card dash-card--compact" aria-labelledby="alert-incidents-title">
      <div className="dash-card__head">
        <div>
          <h2 id="alert-incidents-title" className="dash-card__title">
            Incidents
          </h2>
          <p className="dash-card__desc">Alerts that fired for this project.</p>
        </div>
        <div className="dash-seg" role="group" aria-label="Incident status">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.id}
              type="button"
              className={`dash-seg__btn${status === f.id ? ' is-active' : ''}`}
              aria-pressed={status === f.id}
              onClick={() => pick(f.id)}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>
      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}
      {items === null ? (
        !error && <p className="dash-empty">Loading…</p>
      ) : items.length === 0 ? (
        <p className="dash-empty">No alerts yet.</p>
      ) : (
        <div className="dash-list">
          {items.map((i) => {
            const delivery = notifyLabel(i.notify_state)
            return (
              <div key={i.id} className="dash-list__row">
                <div className="dash-list__main">
                  <div className="dash-list__top">
                    <span className={`badge ${i.status === 'firing' ? 'badge--exception' : 'badge--revoked'}`}>
                      {ruleTitle(i.rule_kind)}
                    </span>
                    {delivery && (
                      <span className={`badge ${i.notify_state === 'pending' ? 'badge--preview' : 'badge--revoked'}`}>
                        {delivery}
                      </span>
                    )}
                    {i.environment !== '' && <span className="dash-code">{i.environment}</span>}
                    {i.release_version !== '' && <span className="dash-code">{i.release_version}</span>}
                    <span className="dash-code">{timeAgo(i.fired_at, now)}</span>
                  </div>
                  <p className="dash-list__title">{incidentHeadline(i)}</p>
                  {i.rule_kind === 'issue_spike' && <p className="dash-list__sub">{i.title}</p>}
                </div>
                <Link to={`/dashboard/issues/${i.issue_id}`} className="dash-btn">
                  Open issue
                </Link>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
