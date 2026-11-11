import { useEffect, useRef, useState } from 'react'
import { alertApi, type AlertBot, type AlertDetailLevel } from '../api'
import {
  botBadge,
  detailLevelLabel,
  isBotEndpointMissing,
  normalizeBot,
  normalizeToken,
  validateBotToken,
} from './alerts/alertFormat'

const LEVELS: AlertDetailLevel[] = ['minimal', 'full']

function errMsg(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

type Props = { orgId: number; canManage: boolean }

export default function TelegramBotCard({ orgId, canManage }: Props) {
  // keyed by org so a switch starts from fresh state (and never keeps a typed token)
  return <BotCardInner key={orgId} orgId={orgId} canManage={canManage} />
}

function BotCardInner({ orgId, canManage }: Props) {
  const [bot, setBot] = useState<AlertBot | null>(null)
  const [error, setError] = useState('')
  const [token, setToken] = useState('')
  const [level, setLevel] = useState<AlertDetailLevel>('minimal')
  const [replacing, setReplacing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [unavailable, setUnavailable] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    alertApi
      .bot(orgId)
      .then((res) => {
        if (cancelled) return
        const norm = normalizeBot(res)
        if (norm === null) {
          setUnavailable(true)
          return
        }
        setBot(norm)
        setLevel(norm.detail_level)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (isBotEndpointMissing(err)) setUnavailable(true)
        else setError(errMsg(err, 'Failed to load the Telegram bot'))
      })
    return () => {
      cancelled = true
    }
  }, [orgId])

  const tokenProblem = token === '' ? null : validateBotToken(normalizeToken(token))

  const connect = async () => {
    const submitted = normalizeToken(token)
    if (busy || validateBotToken(submitted) !== null) return
    setToken('') // cleared before the request: never kept while it is in flight
    setBusy(true)
    setError('')
    try {
      const res = await alertApi.putBot(orgId, { token: submitted, detail_level: level })
      if (!mounted.current) return
      setBot(res)
      setLevel(res.detail_level)
      setReplacing(false)
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to connect the bot'))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  const changeLevel = async (next: AlertDetailLevel) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      const res = await alertApi.patchBot(orgId, { detail_level: next })
      if (!mounted.current) return
      setBot(res)
      setLevel(res.detail_level)
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to update the detail level'))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  const remove = async () => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await alertApi.deleteBot(orgId)
      const res = await alertApi.bot(orgId)
      if (!mounted.current) return
      setBot(res)
      setLevel(res.detail_level)
      setReplacing(false)
      setConfirmDelete(false)
      setToken('')
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to remove the bot'))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  const tokenForm = (
    <div className="dash-form" style={{ marginTop: 'var(--space-sm)' }}>
      <div className="alert-form-field">
        <label htmlFor="tg-token-input" className="alert-form-field__label">
          Telegram Bot Token
        </label>
        <input
          id="tg-token-input"
          type="password"
          className={`dash-input alert-form-field__input${tokenProblem ? ' is-invalid' : ''}`}
          aria-label="Telegram bot token"
          aria-describedby={tokenProblem ? 'tg-bot-token-error' : undefined}
          autoComplete="new-password"
          data-1p-ignore
          data-lpignore="true"
          data-bwignore
          data-form-type="other"
          spellCheck={false}
          placeholder="e.g. 123456789:AAEhBP0av28kPo..."
          value={token}
          disabled={busy}
          onChange={(e) => setToken(e.target.value)}
        />
        {tokenProblem && (
          <p id="tg-bot-token-error" className="dash-error alert-form-field__error">
            {tokenProblem}
          </p>
        )}
      </div>

      {!bot?.configured && (
        <div className="alert-form-field">
          <label htmlFor="tg-level-select" className="alert-form-field__label">
            Alert Message Detail Level
          </label>
          <select
            id="tg-level-select"
            className="dash-input"
            aria-label="Alert detail level"
            value={level}
            disabled={busy}
            onChange={(e) => setLevel(e.target.value as AlertDetailLevel)}
          >
            {LEVELS.map((l) => (
              <option key={l} value={l}>
                {detailLevelLabel(l)}
              </option>
            ))}
          </select>
        </div>
      )}

      <div className="dash-actions" style={{ justifyContent: 'flex-start' }}>
        <button
          type="button"
          className="dash-btn dash-btn--primary"
          disabled={busy || token === '' || tokenProblem !== null}
          onClick={() => void connect()}
        >
          {busy ? 'Connecting…' : bot?.configured ? 'Update Bot Token' : 'Connect Telegram Bot'}
        </button>
        {replacing && (
          <button
            type="button"
            className="dash-btn"
            disabled={busy}
            onClick={() => {
              setReplacing(false)
              setToken('')
            }}
          >
            Cancel
          </button>
        )}
      </div>
    </div>
  )

  const badge = bot ? botBadge(bot) : null

  return (
    <section className="dash-card dash-card--compact" aria-labelledby="tg-bot-title">
      <div className="dash-card__head">
        <div>
          <h2 id="tg-bot-title" className="dash-card__title">
            Organization Telegram Bot
          </h2>
          <p className="dash-card__desc">Connect a dedicated bot to deliver error alerts to your team channels.</p>
        </div>
      </div>

      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {unavailable ? (
        <div className="alert-callout">
          <p className="dash-empty">Telegram bot settings are not available in this deployment.</p>
        </div>
      ) : bot === null ? (
        !error && <p className="dash-empty">Loading bot configuration…</p>
      ) : bot.mode === 'shared' ? (
        <div className="alert-callout">
          <p className="dash-empty">The Telegram alert bot is managed centrally by the server in this deployment.</p>
        </div>
      ) : !bot.configured ? (
        <>
          <div className="bot-guide-box">
            <h3 className="bot-guide-box__title">How to set up your Telegram Bot</h3>
            <ol className="bot-guide-steps">
              <li>Open Telegram and search for <code>@BotFather</code>.</li>
              <li>Send the command <code>/newbot</code> and follow the instructions to set a display name and username (e.g. <code>MyOrgAlertBot</code>).</li>
              <li>Copy the HTTP API token provided by BotFather and paste it below.</li>
            </ol>
          </div>

          {canManage ? tokenForm : <p className="dash-empty">An organisation admin needs to connect a Telegram bot.</p>}
        </>
      ) : (
        <>
          <div className="bot-connected-card">
            <div className="bot-connected-card__left">
              <div className="bot-connected-card__icon" aria-hidden="true">
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2zm4.5 7.5-2 9a.75.75 0 0 1-1.2.3l-2.8-2.3-1.6 1.5a.5.5 0 0 1-.9-.3l.3-3.6 6.5-6a.5.5 0 0 0-.7-.7l-8 5.2-3.1-1a.75.75 0 0 1 0-1.4l12-4.5a.75.75 0 0 1 1 .9z" />
                </svg>
              </div>
              <div className="bot-connected-card__meta">
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <h3 className="bot-connected-card__handle">@{bot.bot_username}</h3>
                  {badge && <span className={`badge ${badge.className}`}>{badge.text}</span>}
                </div>
                <span className="dash-cell-sub">
                  Detail Level: {detailLevelLabel(bot.detail_level)}
                </span>
              </div>
            </div>

            <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-xs)', flexWrap: 'wrap' }}>
              {canManage && (
                <>
                  <div style={{ minWidth: 160 }}>
                    <select
                      className="dash-input"
                      aria-label="Alert detail level"
                      value={bot.detail_level}
                      disabled={busy}
                      onChange={(e) => void changeLevel(e.target.value as AlertDetailLevel)}
                    >
                      {LEVELS.map((l) => (
                        <option key={l} value={l}>
                          {detailLevelLabel(l)}
                        </option>
                      ))}
                    </select>
                  </div>

                  {!replacing && !confirmDelete && (
                    <>
                      <button
                        type="button"
                        className="dash-btn"
                        disabled={busy}
                        onClick={() => setReplacing(true)}
                      >
                        Replace Token
                      </button>
                      <button
                        type="button"
                        className="dash-btn"
                        disabled={busy}
                        onClick={() => setConfirmDelete(true)}
                      >
                        Disconnect
                      </button>
                    </>
                  )}

                  {confirmDelete && (
                    <>
                      <button
                        type="button"
                        className="dash-btn dash-btn--danger"
                        disabled={busy}
                        onClick={() => void remove()}
                      >
                        Confirm Disconnect
                      </button>
                      <button
                        type="button"
                        className="dash-btn"
                        disabled={busy}
                        onClick={() => setConfirmDelete(false)}
                      >
                        Cancel
                      </button>
                    </>
                  )}
                </>
              )}
            </div>
          </div>

          {canManage && replacing && tokenForm}
        </>
      )}
    </section>
  )
}
