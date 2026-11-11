import { useEffect, useRef, useState } from 'react'
import { alertApi, type AlertBot, type AlertDetailLevel } from '../api'
import { botBadge, detailLevelLabel, isBotEndpointMissing, normalizeBot, validateBotToken } from './alerts/alertFormat'

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

  const tokenProblem = token === '' ? null : validateBotToken(token)

  const connect = async () => {
    if (busy || validateBotToken(token) !== null) return
    const submitted = token
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
    if (!window.confirm('Remove the Telegram bot? Alerts will stop until a new bot is connected.')) return
    setBusy(true)
    setError('')
    try {
      await alertApi.deleteBot(orgId)
      const res = await alertApi.bot(orgId)
      if (!mounted.current) return
      setBot(res)
      setLevel(res.detail_level)
      setReplacing(false)
      setToken('')
    } catch (err) {
      if (mounted.current) setError(errMsg(err, 'Failed to remove the bot'))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  const tokenForm = (
    <div className="dash-form">
      <input
        type="password"
        className="dash-input"
        aria-label="Telegram bot token"
        aria-describedby={tokenProblem ? 'tg-bot-token-error' : undefined}
        autoComplete="off"
        spellCheck={false}
        placeholder="123456789:AA..."
        value={token}
        disabled={busy}
        onChange={(e) => setToken(e.target.value)}
      />
      {tokenProblem && (
        <p id="tg-bot-token-error" className="dash-error">
          {tokenProblem}
        </p>
      )}
      {!bot?.configured && (
        <select
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
      )}
      <div className="dash-list__top">
        <button
          type="button"
          className="dash-btn"
          disabled={busy || token === '' || tokenProblem !== null}
          onClick={() => void connect()}
        >
          {busy ? 'Connecting…' : bot?.configured ? 'Replace bot' : 'Connect'}
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
            Telegram bot
          </h2>
          <p className="dash-card__desc">The bot that sends alerts for this organisation.</p>
        </div>
      </div>

      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {unavailable ? (
        <p className="dash-empty">The Telegram bot settings are not available on this server.</p>
      ) : bot === null ? (
        !error && <p className="dash-empty">Loading…</p>
      ) : bot.mode === 'shared' ? (
        <p className="dash-empty">The Telegram bot is managed by the server in this deployment.</p>
      ) : !bot.configured ? (
        <>
          <p className="dash-list__sub">
            Create a bot with @BotFather in Telegram, then copy its token. Alerts are sent from your own bot.
          </p>
          {canManage ? tokenForm : <p className="dash-empty">An organisation admin needs to connect a Telegram bot.</p>}
        </>
      ) : (
        <>
          <div className="dash-list__row">
            <div className="dash-list__main">
              <div className="dash-list__top">
                <p className="dash-list__title">@{bot.bot_username}</p>
                {badge && <span className={`badge ${badge.className}`}>{badge.text}</span>}
              </div>
              {canManage ? (
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
              ) : (
                <p className="dash-list__sub">{detailLevelLabel(bot.detail_level)}</p>
              )}
            </div>
            {canManage && (
              <div className="dash-list__top">
                <button type="button" className="dash-btn" disabled={busy || replacing} onClick={() => setReplacing(true)}>
                  Replace
                </button>
                <button type="button" className="dash-btn" disabled={busy} onClick={() => void remove()}>
                  Remove
                </button>
              </div>
            )}
          </div>
          {canManage && replacing && tokenForm}
        </>
      )}
    </section>
  )
}
