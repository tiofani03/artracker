import { useCallback, useEffect, useRef, useState } from 'react'
import {
  alertApi,
  type AlertChannel,
  type AlertIncident,
  type AlertLinkCode,
  type AlertRule,
  type DiscoveredChat,
} from '../../api'
import { Link } from '../../router'
import {
  BOT_SETTINGS_PATH,
  CHAT_ID_HELP,
  SPIKE_WINDOWS,
  buildRulePatch,
  canShowTest,
  channelActivityLabel,
  countActiveChannels,
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

export type AlertTab = 'incidents' | 'rules' | 'channels'

function getInitialTab(): AlertTab {
  if (typeof window === 'undefined') return 'incidents'
  const t = new URLSearchParams(window.location.search).get('tab')
  if (t === 'rules' || t === 'channels' || t === 'incidents') return t
  return 'incidents'
}

type Props = { projectId: number; canManage: boolean }
type ViewProps = Props & { orgId: number }

export default function AlertsView({ projectId, orgId, canManage }: ViewProps) {
  const [activeTab, setActiveTab] = useState<AlertTab>(getInitialTab)
  const [channelsData, setChannelsData] = useState<NormalizedChannels | null>(null)
  const [rulesData, setRulesData] = useState<AlertRule[] | null>(null)
  const [firingCount, setFiringCount] = useState<number | null>(null)

  const switchTab = (next: AlertTab) => {
    setActiveTab(next)
    if (typeof window !== 'undefined') {
      const url = new URL(window.location.href)
      url.searchParams.set('tab', next)
      window.history.replaceState(null, '', url.pathname + url.search)
    }
  }

  const activeChannelsCount = channelsData ? countActiveChannels(channelsData.items) : 0
  const enabledRulesCount = rulesData ? rulesData.filter((r) => r.enabled).length : 0

  return (
    <div className="dash-panel-stack">
      {/* Sub-header Navigation */}
      <div className="dash-page-header">
        <div className="dash-page-header__title-wrap">
          <h1 className="dash-page-header__title">Alerting Center</h1>
          <p className="dash-page-header__desc">
            Monitor real-time incident triggers and configure Telegram delivery channels.
          </p>
        </div>

        <div className="dash-page-tabs" role="tablist" aria-label="Alerts sections">
          <button
            type="button"
            id="tab-btn-incidents"
            role="tab"
            aria-selected={activeTab === 'incidents'}
            aria-controls="alert-tab-incidents"
            className={`dash-page-tab${activeTab === 'incidents' ? ' is-active' : ''}`}
            onClick={() => switchTab('incidents')}
          >
            <span>Incidents</span>
            {firingCount !== null && firingCount > 0 ? (
              <span className="dash-page-tab__badge dash-page-tab__badge--danger">
                {firingCount}
              </span>
            ) : null}
          </button>
          <button
            type="button"
            id="tab-btn-rules"
            role="tab"
            aria-selected={activeTab === 'rules'}
            aria-controls="alert-tab-rules"
            className={`dash-page-tab${activeTab === 'rules' ? ' is-active' : ''}`}
            onClick={() => switchTab('rules')}
          >
            <span>Trigger Rules</span>
            {rulesData ? (
              <span className="dash-page-tab__badge">
                {enabledRulesCount}/{rulesData.length}
              </span>
            ) : null}
          </button>
          <button
            type="button"
            id="tab-btn-channels"
            role="tab"
            aria-selected={activeTab === 'channels'}
            aria-controls="alert-tab-channels"
            className={`dash-page-tab${activeTab === 'channels' ? ' is-active' : ''}`}
            onClick={() => switchTab('channels')}
          >
            <span>Telegram Channels</span>
            {channelsData ? (
              <span className="dash-page-tab__badge">
                {activeChannelsCount}
              </span>
            ) : null}
          </button>
        </div>
      </div>

      {/* Tab Panels: Only one visible at a time */}
      <div
        role="tabpanel"
        id="alert-tab-incidents"
        aria-labelledby="tab-btn-incidents"
        hidden={activeTab !== 'incidents'}
      >
        <IncidentsCard
          key={`i${projectId}`}
          projectId={projectId}
          onFiringCount={setFiringCount}
        />
      </div>

      <div
        role="tabpanel"
        id="alert-tab-rules"
        aria-labelledby="tab-btn-rules"
        hidden={activeTab !== 'rules'}
      >
        <RulesCard
          key={`r${projectId}`}
          projectId={projectId}
          canManage={canManage}
          onRulesChange={setRulesData}
        />
      </div>

      <div
        role="tabpanel"
        id="alert-tab-channels"
        aria-labelledby="tab-btn-channels"
        hidden={activeTab !== 'channels'}
      >
        <ChannelsCard
          key={`c${orgId}-${projectId}`}
          projectId={projectId}
          canManage={canManage}
          onChannelsChange={setChannelsData}
        />
      </div>
    </div>
  )
}

type LinkPhase = 'active' | 'connected' | 'expired'
type LinkState = { code: AlertLinkCode; before: AlertChannel[]; phase: LinkPhase }
const NO_CHANNELS: AlertChannel[] = []

type ChannelsCardProps = Props & {
  onChannelsChange?: (data: NormalizedChannels) => void
}

function ChannelsCard({ projectId, canManage, onChannelsChange }: ChannelsCardProps) {
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
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)
  const [discovering, setDiscovering] = useState(false)
  const [discoveredChats, setDiscoveredChats] = useState<DiscoveredChat[] | null>(null)
  const [discoverError, setDiscoverError] = useState('')
  const [chatSearchFilter, setChatSearchFilter] = useState('')
  const [showManualForm, setShowManualForm] = useState(false)
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
      const norm = normalizeChannelsResponse(res)
      setData(norm)
      onChannelsChange?.(norm)
      setError('')
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to load channels'))
    }
  }, [projectId, onChannelsChange])

  useEffect(() => {
    let cancelled = false
    alertApi
      .channels(projectId)
      .then((res) => {
        if (cancelled) return
        const norm = normalizeChannelsResponse(res)
        setData(norm)
        onChannelsChange?.(norm)
        setNow(Date.now())
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err, 'Failed to load channels'))
      })
    return () => {
      cancelled = true
    }
  }, [projectId, onChannelsChange])

  const linkActive = link?.phase === 'active'
  const expiresAt = link ? new Date(link.code.expires_at).getTime() : 0
  const before = link?.before ?? NO_CHANNELS

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
        onChannelsChange?.(norm)
        if (detectLinked(before, norm.items)) {
          setLink((l) => (l && l.phase === 'active' ? { ...l, phase: 'connected' } : l))
        }
      } catch {
        // transient poll failure
      }
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(tick)
      clearInterval(poll)
    }
  }, [linkActive, expiresAt, before, projectId, onChannelsChange])

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

  const test = (ch: AlertChannel) =>
    guarded(ch.id, async () => {
      setTestResult((r) => ({ ...r, [ch.id]: 'Sending…' }))
      let result = 'Sent ✓'
      try {
        await alertApi.testChannel(projectId, ch.id)
      } catch (err) {
        result = errMsg(err, 'Test failed')
      }
      if (mounted.current) setTestResult((r) => ({ ...r, [ch.id]: result }))
    })

  const toggle = (ch: AlertChannel) =>
    guarded(ch.id, async () => {
      try {
        await alertApi.setChannelEnabled(projectId, ch.id, !ch.enabled)
        await load()
      } catch (err) {
        if (mounted.current) setError(errMsg(err, 'Failed to update channel'))
      }
    })

  const remove = (ch: AlertChannel) =>
    guarded(ch.id, async () => {
      try {
        await alertApi.deleteChannel(projectId, ch.id)
        setConfirmDeleteId(null)
        await load()
      } catch (err) {
        if (mounted.current) setError(errMsg(err, 'Failed to remove channel'))
      }
    })

  const fetchDiscoveredChats = async () => {
    setDiscovering(true)
    setDiscoverError('')
    try {
      const res = await alertApi.discoverChats(projectId)
      if (!mounted.current) return
      setDiscoveredChats(res.chats)
      if (res.chats.length === 0) {
        setDiscoverError(
          `Belum ada chat yang terdeteksi berinteraksi dengan bot. Silakan undang bot (@${data?.bot?.bot_username || 'bot Anda'}) ke grup Telegram atau kirim pesan ke bot, lalu klik "Cari Chat Lagi".`
        )
      }
    } catch (err) {
      if (mounted.current) {
        setDiscoverError(errMsg(err, 'Gagal mendeteksi chat dari Telegram'))
      }
    } finally {
      if (mounted.current) setDiscovering(false)
    }
  }

  const selectDiscoveredChat = (dc: DiscoveredChat) => {
    setChat(dc.chat_id)
    setLabel(dc.title)
    setShowManualForm(true)
  }

  const addDiscoveredChatDirectly = async (dc: DiscoveredChat) => {
    setAdding(true)
    setError('')
    setAddWarning('')
    try {
      const res = await alertApi.createChannel(projectId, { chat: dc.chat_id, label: dc.title })
      if (!mounted.current) return
      setChat('')
      setLabel('')
      await load()
      setDiscoveredChats((prev) => (prev ? prev.filter((c) => c.chat_id !== dc.chat_id) : null))
      if (res.warning) {
        setAddWarning(res.warning)
      }
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to add channel'))
    } finally {
      if (mounted.current) setAdding(false)
    }
  }

  const banner = data ? channelsBanner(data, canManage) : null
  const chatProblem = chat === '' ? null : validateChat(chat)
  const labelProblem = label === '' ? null : validateLabel(label)
  const activity = (ch: AlertChannel) => (data ? channelActivityLabel(ch, data.mode) : null)

  return (
    <section className="dash-card" aria-labelledby="alert-channels-title">
      <div className="dash-card__head">
        <div>
          <h2 id="alert-channels-title" className="dash-card__title">
            Telegram Notification Channels
          </h2>
          <p className="dash-card__desc">Manage chats that receive real-time notifications for this project.</p>
        </div>
        {canManage && data && showLinkFlow(data.mode) && data.telegram.configured && (
          <button type="button" className="dash-btn dash-btn--primary" onClick={() => void connect()} disabled={creating || linkActive}>
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
        !error && <p className="dash-empty">Loading channels…</p>
      ) : showLinkFlow(data.mode) && !data.telegram.configured ? (
        <p className="dash-empty">Telegram is not configured on this server. Set TELEGRAM_BOT_TOKEN.</p>
      ) : (
        <>
          {banner && (
            <div className="alert-callout alert-callout--warn" role="status" style={{ marginBottom: 'var(--space-md)' }}>
              <div className="alert-callout__icon" aria-hidden="true">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <circle cx="12" cy="12" r="10" />
                  <line x1="12" y1="8" x2="12" y2="12" />
                  <line x1="12" y1="16" x2="12.01" y2="16" />
                </svg>
              </div>
              <div className="alert-callout__body">
                <p className="alert-callout__text">{banner}</p>
              </div>
              {canManage && (
                <Link to={BOT_SETTINGS_PATH} className="dash-btn dash-btn--sm alert-callout__action">
                  Configure Bot →
                </Link>
              )}
            </div>
          )}

          {canManage && !showLinkFlow(data.mode) && (
            <div className="alert-add-form-wrap">
              {/* Auto Chat Discovery Section */}
              <div className="alert-discover-box">
                <div className="alert-discover-box__head">
                  <div>
                    <h3 className="alert-discover-box__title">
                      🔍 Cari &amp; Deteksi Chat Telegram Otomatis
                    </h3>
                    <p className="alert-discover-box__desc">
                      Deteksi otomatis grup atau chat yang berinteraksi dengan bot tanpa perlu input Chat ID manual.
                    </p>
                  </div>
                  <div className="alert-discover-box__actions">
                    {data?.bot?.bot_username && (
                      <a
                        href={`https://t.me/${data.bot.bot_username}?startgroup=alerts`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="dash-btn dash-btn--sm"
                        title="Undang bot ke grup Telegram"
                      >
                        + Undang @{data.bot.bot_username} ke Grup
                      </a>
                    )}
                    <button
                      type="button"
                      className="dash-btn dash-btn--primary"
                      onClick={() => void fetchDiscoveredChats()}
                      disabled={discovering}
                    >
                      {discovering ? 'Mencari Chat…' : '🔍 Cari Chat dari Bot'}
                    </button>
                  </div>
                </div>

                {discoverError && (
                  <p className="dash-warning" role="alert" style={{ margin: 0 }}>
                    {discoverError}
                  </p>
                )}

                {discoveredChats !== null && (
                  <div style={{ marginTop: 'var(--space-2xs)' }}>
                    {discoveredChats.length > 1 && (
                      <div className="alert-discover-search-row">
                        <input
                          type="search"
                          className="dash-input alert-discover-search"
                          placeholder="Filter nama chat…"
                          value={chatSearchFilter}
                          onChange={(e) => setChatSearchFilter(e.target.value)}
                        />
                        <span className="dash-note">{discoveredChats.length} chat ditemukan</span>
                      </div>
                    )}

                    {discoveredChats.length === 0 ? (
                      <p className="dash-note" style={{ margin: 'var(--space-xs) 0 0' }}>
                        Tidak ada chat baru ditemukan. Pastikan bot sudah diundang ke grup atau dikirimi pesan di Telegram.
                      </p>
                    ) : (
                      <div className="alert-discovered-list">
                        {discoveredChats
                          .filter(
                            (dc) =>
                              !chatSearchFilter.trim() ||
                              dc.title.toLowerCase().includes(chatSearchFilter.toLowerCase()) ||
                              dc.chat_id.includes(chatSearchFilter)
                          )
                          .map((dc) => (
                            <div key={dc.chat_id} className="alert-discovered-item">
                              <div className="alert-discovered-item__info">
                                <div className="alert-discovered-item__title-row">
                                  <span className="alert-discovered-item__title">{dc.title}</span>
                                  <span className="badge badge--preview">
                                    {dc.type || 'chat'}
                                  </span>
                                </div>
                                <span className="alert-discovered-item__meta">
                                  ID: <code>{dc.chat_id}</code>{dc.username ? ` · @${dc.username}` : ''}
                                </span>
                              </div>
                              <div className="alert-discover-box__actions">
                                <button
                                  type="button"
                                  className="dash-btn dash-btn--sm dash-btn--primary"
                                  onClick={() => void addDiscoveredChatDirectly(dc)}
                                  disabled={adding}
                                >
                                  {adding ? 'Menambahkan…' : '+ Tambah Langsung'}
                                </button>
                                <button
                                  type="button"
                                  className="dash-btn dash-btn--sm"
                                  onClick={() => selectDiscoveredChat(dc)}
                                >
                                  Edit di Form
                                </button>
                              </div>
                            </div>
                          ))}
                      </div>
                    )}
                  </div>
                )}
              </div>

              {/* Toggle manual form */}
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 'var(--space-sm)' }}>
                <button
                  type="button"
                  className="dash-btn dash-btn--sm"
                  onClick={() => setShowManualForm((prev) => !prev)}
                >
                  {showManualForm ? 'Tutup Input Manual' : 'Atau Input ID Manual / @channel'}
                </button>
              </div>

              {showManualForm && (
                <>
                  <details className="alert-help-disclosure">
                    <summary className="alert-help-disclosure__summary">
                      <span className="alert-help-disclosure__title">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                          <circle cx="12" cy="12" r="10" />
                          <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3" />
                          <line x1="12" y1="17" x2="12.01" y2="17" />
                        </svg>
                        How to find the Telegram Chat ID
                      </span>
                      <span className="alert-help-disclosure__badge">Guide</span>
                    </summary>
                    <div className="alert-help-disclosure__content">
                      <p className="dash-list__sub" id="alert-add-chat-help">
                        {CHAT_ID_HELP}
                      </p>
                    </div>
                  </details>

                  <form
                    className="alert-add-form"
                    onSubmit={(e) => {
                      e.preventDefault()
                      void addChat()
                    }}
                  >
                    <div className="alert-add-form__grid">
                      <div className="alert-form-field">
                        <label htmlFor="alert-chat-input" className="alert-form-field__label">
                          Chat ID or @channel
                        </label>
                        <input
                          id="alert-chat-input"
                          type="text"
                          className={`dash-input alert-form-field__input${chatProblem ? ' is-invalid' : ''}`}
                          aria-label="Chat id or @channel_username"
                          aria-describedby={chatProblem ? 'alert-add-chat-error' : 'alert-add-chat-help'}
                          placeholder="-1001234567890 or @channel"
                          value={chat}
                          disabled={adding}
                          onChange={(e) => setChat(e.target.value)}
                        />
                        {chatProblem && (
                          <p id="alert-add-chat-error" className="dash-error alert-form-field__error">
                            {chatProblem}
                          </p>
                        )}
                      </div>

                      <div className="alert-form-field">
                        <label htmlFor="alert-label-input" className="alert-form-field__label">
                          Chat Label
                        </label>
                        <input
                          id="alert-label-input"
                          type="text"
                          className={`dash-input alert-form-field__input${labelProblem ? ' is-invalid' : ''}`}
                          aria-label="Chat label"
                          aria-describedby={labelProblem ? 'alert-add-label-error' : undefined}
                          placeholder="e.g. On-call Ops"
                          value={label}
                          disabled={adding}
                          onChange={(e) => setLabel(e.target.value)}
                        />
                        {labelProblem && (
                          <p id="alert-add-label-error" className="dash-error alert-form-field__error">
                            {labelProblem}
                          </p>
                        )}
                      </div>

                      <button
                        type="submit"
                        className="dash-btn dash-btn--primary alert-add-form__submit"
                        disabled={adding || validateChat(chat) !== null || validateLabel(label) !== null}
                      >
                        {adding ? 'Adding…' : '+ Add Chat'}
                      </button>
                    </div>

                    {addWarning && (
                      <p className="dash-warning alert-form-field__warning" aria-live="polite">
                        {addWarning}
                      </p>
                    )}
                  </form>
                </>
              )}
            </div>
          )}

          {link && showLinkFlow(data.mode) && (
            <div className="dash-list__row" aria-live="polite">
              <div className="dash-list__main">
                {link.phase === 'active' && (
                  <>
                    <div className="dash-list__top">
                      <a href={link.code.group_link} target="_blank" rel="noopener noreferrer" className="dash-btn dash-btn--primary">
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
            <div className="alert-empty-state">
              <div className="alert-empty-state__icon" aria-hidden="true">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
                  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                </svg>
              </div>
              <p className="alert-empty-state__title">No channels connected</p>
              <p className="alert-empty-state__desc">
                Add a Telegram group or private chat above to receive alerts when incidents fire.
              </p>
            </div>
          ) : (
            <div className="dash-list">
              {data.items.map((ch) => (
                <div key={ch.id} className="dash-list__row alert-channel-row">
                  <div className="dash-list__main">
                    <div className="dash-list__top">
                      <span className={`badge ${ch.enabled ? 'badge--active' : 'badge--revoked'}`}>
                        {ch.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                      <span className="dash-code">{ch.chat_type}</span>
                      {activity(ch) && <span className="dash-code">{activity(ch)}</span>}
                      {!ch.enabled && ch.disabled_reason && <span className="dash-code">{ch.disabled_reason}</span>}
                      {ch.last_sent_at && <span className="dash-code">Last sent {timeAgo(ch.last_sent_at, new Date(now))}</span>}
                      {testResult[ch.id] && (
                        <span
                          className={`alert-test-status ${testResult[ch.id].includes('✓') ? 'is-success' : 'is-error'}`}
                          aria-live="polite"
                        >
                          {testResult[ch.id]}
                        </span>
                      )}
                    </div>
                    <p className="dash-list__title">{ch.title}</p>
                    {ch.destination !== '' && <p className="dash-list__sub dash-code">{ch.destination}</p>}
                    {ch.last_error !== '' && (
                      <div className="alert-channel-error-callout" role="alert">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                          <circle cx="12" cy="12" r="10" />
                          <line x1="12" y1="8" x2="12" y2="12" />
                          <line x1="12" y1="16" x2="12.01" y2="16" />
                        </svg>
                        <span>Delivery issue: {ch.last_error}</span>
                      </div>
                    )}
                  </div>
                  {canManage && (
                    <div className="dash-list__top">
                      {confirmDeleteId === ch.id ? (
                        <>
                          <button
                            type="button"
                            className="dash-btn dash-btn--danger"
                            disabled={pending.has(ch.id)}
                            onClick={() => void remove(ch)}
                          >
                            Confirm Remove
                          </button>
                          <button
                            type="button"
                            className="dash-btn"
                            disabled={pending.has(ch.id)}
                            onClick={() => setConfirmDeleteId(null)}
                          >
                            Cancel
                          </button>
                        </>
                      ) : (
                        <>
                          {canShowTest(ch) && (
                            <button
                              type="button"
                              className="dash-btn"
                              disabled={pending.has(ch.id)}
                              onClick={() => void test(ch)}
                            >
                              Test
                            </button>
                          )}
                          <button
                            type="button"
                            className="dash-btn"
                            disabled={pending.has(ch.id)}
                            onClick={() => void toggle(ch)}
                          >
                            {ch.enabled ? 'Disable' : 'Enable'}
                          </button>
                          <button
                            type="button"
                            className="dash-btn"
                            disabled={pending.has(ch.id)}
                            onClick={() => setConfirmDeleteId(ch.id)}
                          >
                            Remove
                          </button>
                        </>
                      )}
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

type RulesCardProps = Props & {
  onRulesChange?: (rules: AlertRule[]) => void
}

function RulesCard({ projectId, canManage, onRulesChange }: RulesCardProps) {
  const [rules, setRules] = useState<AlertRule[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    alertApi
      .rules(projectId)
      .then((res) => {
        if (!cancelled) {
          setRules(res.items)
          onRulesChange?.(res.items)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err, 'Failed to load rules'))
      })
    return () => {
      cancelled = true
    }
  }, [projectId, onRulesChange])

  const replace = (updated: AlertRule) =>
    setRules((cur) => {
      const next = cur ? cur.map((r) => (r.kind === updated.kind ? updated : r)) : cur
      if (next) onRulesChange?.(next)
      return next
    })

  return (
    <section className="dash-card" aria-labelledby="alert-rules-title">
      <div className="dash-card__head">
        <div>
          <h2 id="alert-rules-title" className="dash-card__title">
            Alert Trigger Rules
          </h2>
          <p className="dash-card__desc">Define condition thresholds for triggering Telegram alerts.</p>
        </div>
      </div>
      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}
      {rules === null ? (
        !error && <p className="dash-empty">Loading rules…</p>
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
    <div className={`alert-rule-row${dirty ? ' is-dirty' : ''}`}>
      <div className="alert-rule-row__header">
        <div className="alert-rule-row__title-wrap">
          <button
            type="button"
            role="switch"
            aria-checked={draft.enabled}
            aria-label={`${title} enabled`}
            className={`dash-switch${draft.enabled ? ' is-on' : ''}`}
            disabled={!canManage || saving}
            onClick={() => set({ enabled: !draft.enabled })}
          >
            <span className="dash-switch__knob" />
          </button>
          <div>
            <h3 className="alert-rule-row__title">{title}</h3>
            <p className="alert-rule-row__desc">{ruleDescription(rule)}</p>
          </div>
        </div>

        {canManage && (
          <div className="alert-rule-actions">
            {dirty && (
              <button
                type="button"
                className="dash-btn"
                disabled={saving}
                onClick={() => setDraft(draftFromRule(rule))}
              >
                Discard
              </button>
            )}
            <button
              type="button"
              className={`dash-btn${dirty ? ' dash-btn--primary' : ''}`}
              disabled={!dirty || problem !== null || saving}
              onClick={() => void save()}
            >
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        )}
      </div>

      <div className="alert-rule-grid">
        <div className="alert-field">
          <label className="alert-field__label" htmlFor={`rule-env-${rule.kind}`}>
            Environment Scope
          </label>
          <input
            id={`rule-env-${rule.kind}`}
            type="text"
            className="dash-input alert-field__input"
            aria-label={`${title} environment`}
            aria-describedby={envHelpId}
            placeholder="all environments"
            maxLength={128}
            value={draft.environment}
            disabled={!canManage || saving}
            onChange={(e) => set({ environment: e.target.value })}
          />
          <span id={envHelpId} className="alert-field__hint">
            Leave blank to match all environments
          </span>
        </div>

        {spike && (
          <>
            <div className="alert-field">
              <label className="alert-field__label" htmlFor={`rule-mul-${rule.kind}`}>
                Multiplier
              </label>
              <div className="alert-input-unit">
                <input
                  id={`rule-mul-${rule.kind}`}
                  type="number"
                  className="dash-input alert-field__input"
                  aria-label="Multiplier"
                  title="Multiplier"
                  step={0.5}
                  value={draft.multiplier}
                  disabled={!canManage || saving}
                  onChange={(e) => set({ multiplier: e.target.value })}
                />
                <span className="alert-input-unit__label">×</span>
              </div>
              <span className="alert-field__hint">vs usual rate</span>
            </div>

            <div className="alert-field">
              <label className="alert-field__label" htmlFor={`rule-count-${rule.kind}`}>
                Min Events
              </label>
              <div className="alert-input-unit">
                <input
                  id={`rule-count-${rule.kind}`}
                  type="number"
                  className="dash-input alert-field__input"
                  aria-label="Minimum events"
                  title="Minimum events"
                  step={1}
                  value={draft.minCount}
                  disabled={!canManage || saving}
                  onChange={(e) => set({ minCount: e.target.value })}
                />
                <span className="alert-input-unit__label">ev</span>
              </div>
              <span className="alert-field__hint">Min threshold</span>
            </div>

            <div className="alert-field">
              <label className="alert-field__label" htmlFor={`rule-win-${rule.kind}`}>
                Window
              </label>
              <select
                id={`rule-win-${rule.kind}`}
                className="dash-input alert-field__input"
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
              <span className="alert-field__hint">Rolling window</span>
            </div>

            <div className="alert-field">
              <label className="alert-field__label" htmlFor={`rule-cool-${rule.kind}`}>
                Cooldown
              </label>
              <div className="alert-input-unit">
                <input
                  id={`rule-cool-${rule.kind}`}
                  type="number"
                  className="dash-input alert-field__input"
                  aria-label="Cooldown minutes"
                  title="Cooldown minutes"
                  step={1}
                  value={draft.cooldown}
                  disabled={!canManage || saving}
                  onChange={(e) => set({ cooldown: e.target.value })}
                />
                <span className="alert-input-unit__label">min</span>
              </div>
              <span className="alert-field__hint">Quiet period</span>
            </div>
          </>
        )}
      </div>

      {problem && <p className="dash-error">{problem}</p>}
      {saveError && (
        <p className="dash-error" role="alert">
          {saveError}
        </p>
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

type IncidentsCardProps = {
  projectId: number
  onFiringCount?: (count: number) => void
}

function IncidentsCard({ projectId, onFiringCount }: IncidentsCardProps) {
  const [status, setStatus] = useState<StatusFilter>('firing')
  const [items, setItems] = useState<AlertIncident[] | null>(null)
  const [now, setNow] = useState(() => new Date())
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    alertApi
      .incidents(projectId, 'firing')
      .then((res) => {
        if (!cancelled) onFiringCount?.(res.items.length)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [projectId, onFiringCount])

  useEffect(() => {
    let cancelled = false
    alertApi
      .incidents(projectId, status)
      .then((res) => {
        if (cancelled) return
        setItems(res.items)
        setNow(new Date())
        setError('')
        if (status === 'firing') {
          onFiringCount?.(res.items.length)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err, 'Failed to load incidents'))
      })
    return () => {
      cancelled = true
    }
  }, [projectId, status, onFiringCount])

  const pick = (next: StatusFilter) => {
    if (next === status) return
    setItems(null)
    setError('')
    setStatus(next)
  }

  return (
    <section className="dash-card" aria-labelledby="alert-incidents-title">
      <div className="dash-card__head">
        <div>
          <h2 id="alert-incidents-title" className="dash-card__title">
            Alert Incidents Feed
          </h2>
          <p className="dash-card__desc">Alert events triggered by your error telemetry.</p>
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
        !error && <p className="dash-empty">Loading incidents…</p>
      ) : items.length === 0 ? (
        <div className="alert-empty-state">
          <div className="alert-empty-state__icon" aria-hidden="true">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
              <path d="m9 12 2 2 4-4" />
            </svg>
          </div>
          <p className="alert-empty-state__title">
            {status === 'firing' ? 'No firing alerts' : status === 'resolved' ? 'No resolved alerts' : 'No alerts recorded'}
          </p>
          <p className="alert-empty-state__desc">
            {status === 'firing'
              ? 'All monitored issues are currently quiet and within expected thresholds.'
              : 'Alert events will appear here when error conditions trigger your configured rules.'}
          </p>
        </div>
      ) : (
        <div className="dash-list">
          {items.map((i) => {
            const delivery = notifyLabel(i.notify_state)
            const isSpike = i.rule_kind === 'issue_spike'
            const spikeSummary = isSpike ? incidentHeadline(i) : null

            return (
              <div key={i.id} className="dash-list__row alert-incident-row">
                <div className="dash-list__main">
                  <div className="dash-list__top">
                    <span
                      className={`badge ${
                        i.status === 'firing'
                          ? 'badge--exception alert-status-badge--firing'
                          : 'badge--active'
                      }`}
                    >
                      <span className="alert-status-dot" aria-hidden="true" />
                      {i.status === 'firing' ? 'Firing' : 'Resolved'}
                    </span>
                    <span className="badge badge--preview">{ruleTitle(i.rule_kind)}</span>
                    {delivery && (
                      <span className={`badge ${i.notify_state === 'pending' ? 'badge--preview' : 'badge--revoked'}`}>
                        {delivery}
                      </span>
                    )}
                    {i.environment !== '' && <span className="dash-code">{i.environment}</span>}
                    {i.release_version !== '' && <span className="dash-code">{i.release_version}</span>}
                    <span className="dash-code">{timeAgo(i.fired_at, now)}</span>
                  </div>

                  <div className="dash-list__top" style={{ marginTop: 2 }}>
                    <p className="dash-list__title">{i.title}</p>
                    {spikeSummary && (
                      <span className="alert-spike-pill" title="Spike detection metrics">
                        {spikeSummary}
                      </span>
                    )}
                  </div>
                </div>
                <Link to={`/dashboard/issues/${i.issue_id}`} className="dash-btn">
                  Open issue →
                </Link>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
