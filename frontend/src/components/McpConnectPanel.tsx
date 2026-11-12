import { useState } from 'react'
import { BASE, keyApi, type CreatedKey, type Project } from '../api'
import { claudeCodeCommand, cursorConfig, mcpApiUrl, mcpServerName } from '../issues/mcpSnippet'
import { useToast } from './Toast'

type Props = { project: Project; onCreated?: () => void }
type Client = 'claude' | 'cursor'

export default function McpConnectPanel({ project, onCreated }: Props) {
  const toast = useToast()
  const [fresh, setFresh] = useState<CreatedKey | null>(null)
  const [client, setClient] = useState<Client>('claude')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const name = mcpServerName(project.slug)
  const url = mcpApiUrl(BASE, window.location.origin)
  const snippet = fresh
    ? client === 'claude'
      ? claudeCodeCommand(url, name, fresh.secret)
      : cursorConfig(url, name, fresh.secret)
    : ''

  const create = async () => {
    setBusy(true)
    setError('')
    try {
      setFresh(await keyApi.create(project.id, 'mcp'))
      onCreated?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create MCP key')
    } finally {
      setBusy(false)
    }
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(snippet)
      toast('Copied to clipboard')
    } catch {
      toast('Copy failed. Select the text and copy it manually.', 'error')
    }
  }

  return (
    <section className="dash-card dash-card--compact" aria-labelledby="mcp-title">
      <div className="dash-card__head">
        <div>
          <h2 id="mcp-title" className="dash-card__title">
            Connect to Claude Code
          </h2>
          <p className="dash-card__desc">
            Let Claude Code or Cursor read crashes and fix them in your repo through MCP, scoped to {project.name}.
          </p>
        </div>
        <button type="button" className="dash-btn dash-btn--primary" onClick={() => void create()} disabled={busy}>
          {busy ? 'Creating…' : 'Create MCP key'}
        </button>
      </div>

      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {fresh && (
        <div className="mcp-connect">
          <p className="dash-warning" role="note">
            This key is shown once. Copy the setup below now; it cannot be displayed again.
          </p>
          <div className="dash-page-tabs" role="tablist" aria-label="MCP client">
            {(['claude', 'cursor'] as const).map((c) => (
              <button
                key={c}
                type="button"
                role="tab"
                aria-selected={client === c}
                className={`dash-page-tab${client === c ? ' is-active' : ''}`}
                onClick={() => setClient(c)}
              >
                {c === 'claude' ? 'Claude Code' : 'Cursor'}
              </button>
            ))}
          </div>
          <p className="dash-card__desc">
            {client === 'claude' ? 'Run this in your terminal:' : <>Add this to <code className="dash-code">.cursor/mcp.json</code>:</>}
          </p>
          <div className="dash-secret mcp-connect__snippet">
            <pre>
              <code>{snippet}</code>
            </pre>
          </div>
          <div className="dash-actions">
            <button type="button" className="dash-btn" onClick={() => void copy()}>
              Copy
            </button>
          </div>
          <p className="dash-note">
            Try it: ask "What are my top crashes?" or run <code className="dash-code">/mcp__{name}__fix_issue</code>
          </p>
        </div>
      )}
    </section>
  )
}
