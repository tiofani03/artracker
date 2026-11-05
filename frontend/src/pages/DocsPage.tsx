import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  BASE,
  getAccessToken,
  keyApi,
  type ApiKey,
} from '../api'
import { useAuth } from '../auth/AuthContext'
import { Link } from '../router'
import '../docs.css'

function renderInline(text: string): ReactNode {
  if (!text) return text
  const parts: ReactNode[] = []
  // Matches **bold**, *italic*, `inline-code`
  const regex = /(\*\*[^*]+\*\*|\*[^*]+\*|`[^`]+`)/g
  let lastIdx = 0
  let match: RegExpExecArray | null

  while ((match = regex.exec(text)) !== null) {
    if (match.index > lastIdx) {
      parts.push(text.substring(lastIdx, match.index))
    }
    const token = match[0]
    if (token.startsWith('**') && token.endsWith('**')) {
      parts.push(<strong key={match.index}>{token.slice(2, -2)}</strong>)
    } else if (token.startsWith('*') && token.endsWith('*')) {
      parts.push(<em key={match.index}>{token.slice(1, -1)}</em>)
    } else if (token.startsWith('`') && token.endsWith('`')) {
      parts.push(
        <code key={match.index} className="fuma-inline-code">
          {token.slice(1, -1)}
        </code>,
      )
    }
    lastIdx = regex.lastIndex
  }

  if (lastIdx < text.length) {
    parts.push(text.substring(lastIdx))
  }

  return parts.length > 0 ? parts : text
}

function MarkdownContent({ content }: { content: string }) {
  const blocks = content.split(/\n\n+/)
  return (
    <div className="fuma-prose">
      {blocks.map((block, idx) => {
        const trimmed = block.trim()
        if (!trimmed) return null

        // H3 Header: ### Title
        if (trimmed.startsWith('### ')) {
          return (
            <h3 key={idx} className="fuma-prose__h3">
              {renderInline(trimmed.replace(/^###\s+/, ''))}
            </h3>
          )
        }

        // H2 Header: ## Title
        if (trimmed.startsWith('## ')) {
          return (
            <h2 key={idx} className="fuma-prose__h2">
              {renderInline(trimmed.replace(/^##\s+/, ''))}
            </h2>
          )
        }

        // Code block: ```lang ... ```
        if (trimmed.startsWith('```')) {
          const lines = trimmed.split('\n')
          const lang = lines[0].replace('```', '').trim()
          const code = lines.slice(1, lines[lines.length - 1].startsWith('```') ? -1 : undefined).join('\n')
          return (
            <div key={idx} className="fuma-code-box">
              {lang && <div className="fuma-code-box__lang">{lang}</div>}
              <pre className="fuma-code-box__pre">
                <code>{code}</code>
              </pre>
            </div>
          )
        }

        // Markdown Table: | Col 1 | Col 2 |
        if (trimmed.startsWith('|') && trimmed.includes('\n|')) {
          const lines = trimmed.split('\n').map((l) => l.trim()).filter(Boolean)
          const parseRow = (line: string) =>
            line
              .split('|')
              .map((c) => c.trim())
              .filter((c, i, a) => !(i === 0 && c === '') && !(i === a.length - 1 && c === ''))

          const headerCols = parseRow(lines[0])
          const bodyRows = lines.slice(2).map(parseRow)

          return (
            <div key={idx} className="fuma-table-wrap">
              <table className="fuma-table">
                <thead>
                  <tr>
                    {headerCols.map((h, hi) => (
                      <th key={hi}>{renderInline(h)}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {bodyRows.map((cols, ri) => (
                    <tr key={ri}>
                      {cols.map((col, ci) => (
                        <td key={ci}>{renderInline(col)}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }

        // Ordered list: 1. ... \n 2. ...
        if (/^\d+\.\s+/.test(trimmed)) {
          const items = trimmed.split('\n').filter((l) => /^\d+\.\s+/.test(l.trim()))
          return (
            <ol key={idx} className="fuma-list-ol">
              {items.map((item, ii) => (
                <li key={ii}>{renderInline(item.replace(/^\d+\.\s+/, ''))}</li>
              ))}
            </ol>
          )
        }

        // Unordered bullet list: - ... \n - ...
        if (/^-\s+/.test(trimmed)) {
          const items = trimmed.split('\n').filter((l) => /^-\s+/.test(l.trim()))
          return (
            <ul key={idx} className="fuma-list-ul">
              {items.map((item, ii) => (
                <li key={ii}>{renderInline(item.replace(/^-\s+/, ''))}</li>
              ))}
            </ul>
          )
        }

        // Normal paragraph
        return (
          <p key={idx} className="fuma-prose__p">
            {renderInline(trimmed)}
          </p>
        )
      })}
    </div>
  )
}

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE'

type FieldParam = {
  name: string
  type: string
  required: boolean
  desc: string
  children?: FieldParam[]
}

type DocSection = {
  id: string
  title: string
  category: string
  method?: HttpMethod
  path?: string
  description: string
  callouts?: { type: 'info' | 'tip' | 'warning'; title: string; content: string }[]
  headers?: { name: string; type: string; required: boolean; desc: string }[]
  params?: FieldParam[]
  responseSample?: string
  defaultHeaders?: Record<string, string>
  defaultBody?: string
  presets?: { name: string; body: string }[]
  isGuide?: boolean
  guideContent?: string
}

const SECTIONS: DocSection[] = [
  {
    id: 'architecture',
    title: 'Architecture & Ingestion SLA',
    category: 'GETTING STARTED',
    isGuide: true,
    description:
      'High-throughput telemetry ingestion pipeline designed for 1,000,000+ daily active users with sub-25ms non-blocking write latency.',
    callouts: [
      {
        type: 'info',
        title: 'SLA Guarantee',
        content:
          'Ingestion writes are non-blocking. The edge validates the payload, enqueues the batch into memory, and responds with HTTP 202 Accepted in under 25ms before persisting to PostgreSQL.',
      },
      {
        type: 'tip',
        title: 'Binary Ingestion Protocol',
        content:
          'Background workers flush telemetry batches using the binary pgx.CopyFrom protocol directly into range-partitioned tables for zero-lock insert throughput.',
      },
    ],
    guideContent: `### Ingestion Pipeline Workflow

ARTracker isolates ingress validation from database persistence:

1. **Ingress & Anti-Zipbomb Verification**: Incoming batches over HTTP/2 are decompressed via pooled gzip readers protected by a 2 MB hard limit to prevent zip-bomb exploits.
2. **Sharded In-Memory Key Cache**: Project API keys are verified in under 0.1ms via an in-memory sharded LRU cache with negative caching and singleflight deduplication.
3. **Per-Project Dual Token Bucket**: Telemetry volume is rate-limited per project across both requests per second (120 RPS) and events per second (1,200 EPS).
4. **Buffered Channel Queue**: Valid envelopes are accepted into an in-memory worker channel and immediately return HTTP 202 Accepted to the client (<25ms SLA).
5. **Worker Pool Batch Flush**: Asynchronous background workers consume envelopes, normalize timestamps for client clock drift, generate UUIDv7 identifiers, and bulk-insert records into range-partitioned PostgreSQL tables using the binary pgx.CopyFrom protocol.`,
  },
  {
    id: 'authentication',
    title: 'Authentication & API Keys',
    category: 'GETTING STARTED',
    isGuide: true,
    description:
      'ARTracker uses two authentication schemes depending on whether requests originate from client-side SDKs or administrative API clients.',
    callouts: [
      {
        type: 'info',
        title: 'SDK Client Keys',
        content:
          'SDK keys start with art_live_ and are validated against the in-memory cache without hitting PostgreSQL on every request.',
      },
      {
        type: 'warning',
        title: 'Rotated Keys',
        content:
          'When rotating an API key in Settings, the old key remains valid during the transition window before full revocation takes effect.',
      },
    ],
    guideContent: `### Client SDK Authentication
SDK clients transmitting telemetry to \`POST /api/v1/ingest\` authenticate via the \`X-Artracker-Key\` header:

\`\`\`http
X-Artracker-Key: art_live_a892b109c3d4e5f6...
\`\`\`

- Keys are scoped per-project and can be rotated or revoked at any time from **Settings**.
- Key verification is cached in memory with a 60-second TTL.

### Administrative API Authentication
Dashboard management endpoints (such as querying project stats or listing event logs) require a Bearer token issued upon Google SSO authentication:

\`\`\`http
Authorization: Bearer eyJhbGciOiJIUzI1Ni...
\`\`\``,
  },
  {
    id: 'ingest-batch',
    title: 'Batch Telemetry Ingest',
    category: 'CORE INGESTION API',
    method: 'POST',
    path: '/ingest',
    description:
      'High-throughput batch ingestion endpoint accepting up to 500 events, error logs, and session heartbeats in a single request. Supports Content-Encoding: gzip.',
    callouts: [
      {
        type: 'tip',
        title: 'Payload Compression',
        content:
          'Pass Content-Encoding: gzip to transmit compressed telemetry batches. The server decompresses payloads via sync.Pool readers without allocation overhead.',
      },
      {
        type: 'warning',
        title: 'Attribute Size Cap',
        content:
          'Individual event attributes exceeding 32 KB are automatically truncated and marked with __truncated: true to protect database storage.',
      },
    ],
    headers: [
      { name: 'X-Artracker-Key', type: 'string', required: true, desc: 'Project client API key starting with art_live_.' },
      { name: 'Content-Type', type: 'string', required: true, desc: 'application/json' },
      { name: 'Content-Encoding', type: 'string', required: false, desc: 'Optional gzip for compressed payloads.' },
    ],
    params: [
      { name: 'sent_at', type: 'string (ISO-8601)', required: false, desc: 'Device wall-clock timestamp when envelope was dispatched. Used to correct client clock skew.' },
      { name: 'sdk', type: 'object', required: false, desc: 'Client library identifier and version string (e.g. { name: "artracker-browser", version: "1.2.0" }).' },
      { name: 'context', type: 'object', required: false, desc: 'Global device/environment metadata: environment, app_version, os, browser, user_agent.' },
      {
        name: 'items',
        type: 'array<RawItem>',
        required: true,
        desc: 'Batch array containing 1 to 500 telemetry items (events, error logs, or session captures).',
        children: [
          { name: 'items[].type', type: 'string', required: true, desc: 'Record classification: "event", "log", or "session".' },
          { name: 'items[].name', type: 'string', required: false, desc: 'Event identifier (e.g. "campaign_attribution", "checkout_completed"). Max 255 chars.' },
          { name: 'items[].level', type: 'string', required: false, desc: 'Severity level for logs: "debug", "info", "warn", "error", or "fatal".' },
          { name: 'items[].message', type: 'string', required: false, desc: 'Error message or console output. Max 4,096 chars.' },
          { name: 'items[].session_id', type: 'string', required: false, desc: 'Session identifier linking events directly to Session Replay timelines.' },
          { name: 'items[].user_id', type: 'string', required: false, desc: 'User identifier or email.' },
          { name: 'items[].timestamp', type: 'string (ISO-8601)', required: false, desc: 'Client event occurrence timestamp. Normalized on ingest.' },
          { name: 'items[].attributes', type: 'object', required: false, desc: 'Custom JSON metadata dictionary (UTM parameters, revenue, order details). Max 32 KB.' },
        ],
      },
    ],
    responseSample: JSON.stringify({ status: 'accepted', queued_items: 2 }, null, 2),
    defaultHeaders: {
      'Content-Type': 'application/json',
      'X-Artracker-Key': 'art_live_xxxxxxxxxxxxxxxx',
    },
    defaultBody: JSON.stringify(
      {
        sent_at: new Date().toISOString(),
        sdk: { name: 'artracker-browser', version: '1.2.0' },
        context: { environment: 'production', app_version: '2.4.1' },
        items: [
          {
            type: 'event',
            name: 'campaign_attribution',
            session_id: 'ses_94829',
            user_id: 'usr_83910',
            timestamp: new Date().toISOString(),
            attributes: {
              utm_source: 'instagram',
              utm_medium: 'reels',
              utm_campaign: 'utm_launch_q4',
              landing_page: '/pricing',
            },
          },
          {
            type: 'event',
            name: 'checkout_completed',
            session_id: 'ses_94829',
            user_id: 'usr_83910',
            timestamp: new Date().toISOString(),
            attributes: {
              plan: 'enterprise',
              billing_cycle: 'annual',
              amount_idr: 4500000,
            },
          },
        ],
      },
      null,
      2,
    ),
    presets: [
      {
        name: 'Standard Batch',
        body: JSON.stringify(
          {
            sent_at: new Date().toISOString(),
            sdk: { name: 'artracker-browser', version: '1.2.0' },
            context: { environment: 'production' },
            items: [
              {
                type: 'event',
                name: 'page_view',
                session_id: `ses_${Date.now()}`,
                attributes: { path: '/pricing', referrer: 'https://google.com' },
              },
              {
                type: 'event',
                name: 'button_clicked',
                session_id: `ses_${Date.now()}`,
                attributes: { button_id: 'btn-signup', label: 'Create Account' },
              },
            ],
          },
          null,
          2,
        ),
      },
      {
        name: 'Campaign Attribution (UTM)',
        body: JSON.stringify(
          {
            sent_at: new Date().toISOString(),
            sdk: { name: 'artracker-web', version: '1.2.0' },
            context: { environment: 'production' },
            items: [
              {
                type: 'event',
                name: 'campaign_attribution',
                session_id: `ses_${Date.now()}`,
                user_id: 'rian.wijaya@commerce.co',
                timestamp: new Date().toISOString(),
                attributes: {
                  utm_source: 'google',
                  utm_medium: 'cpc',
                  utm_campaign: 'utm_launch_q4',
                  utm_term: 'realtime error tracking',
                  revenue: 1250000,
                },
              },
              {
                type: 'event',
                name: 'checkout_completed',
                session_id: `ses_${Date.now()}`,
                attributes: { order_id: 'ORD-9841', items_count: 2, total_idr: 1250000 },
              },
            ],
          },
          null,
          2,
        ),
      },
      {
        name: 'Unhandled Error Log',
        body: JSON.stringify(
          {
            sent_at: new Date().toISOString(),
            sdk: { name: 'artracker-web', version: '1.2.0' },
            context: { environment: 'production', release: 'v2.4.1' },
            items: [
              {
                type: 'log',
                level: 'error',
                message: 'TypeError: Cannot read properties of undefined (reading "checkoutData")',
                session_id: `ses_${Date.now()}`,
                user_id: 'usr_1029',
                timestamp: new Date().toISOString(),
                attributes: {
                  filename: 'PaymentModal.tsx',
                  lineno: 142,
                  colno: 18,
                  stack: 'TypeError: Cannot read properties of undefined\n    at PaymentModal.tsx:142:18\n    at onClick (Button.tsx:24:9)',
                  route: '/checkout/payment',
                },
              },
            ],
          },
          null,
          2,
        ),
      },
      {
        name: 'Network 502 Span',
        body: JSON.stringify(
          {
            sent_at: new Date().toISOString(),
            sdk: { name: 'artracker-web', version: '1.2.0' },
            context: { environment: 'production' },
            items: [
              {
                type: 'event',
                name: 'network_http_error',
                session_id: `ses_${Date.now()}`,
                timestamp: new Date().toISOString(),
                attributes: {
                  url: 'https://api.payment.dev/v1/charge',
                  method: 'POST',
                  status: 502,
                  status_text: 'Bad Gateway',
                  duration_ms: 2410,
                },
              },
            ],
          },
          null,
          2,
        ),
      },
    ],
  },
  {
    id: 'validate-key',
    title: 'Validate Client Key',
    category: 'CORE INGESTION API',
    method: 'GET',
    path: '/sdk/validate',
    description:
      'Low-latency key validation endpoint. Validates client credentials against the in-memory key cache without hitting PostgreSQL.',
    headers: [
      { name: 'X-Artracker-Key', type: 'string', required: true, desc: 'Project client API key to validate.' },
    ],
    responseSample: JSON.stringify({ valid: true, project_id: 1 }, null, 2),
    defaultHeaders: {
      'X-Artracker-Key': 'art_live_xxxxxxxxxxxxxxxx',
    },
  },
  {
    id: 'get-stats',
    title: 'Get Project Statistics',
    category: 'TELEMETRY QUERY API',
    method: 'GET',
    path: '/projects/:projectId/stats?range=24h',
    description:
      'Aggregates session counts, total events, error volumes, crash-free rates, and time-series buckets from PostgreSQL.',
    headers: [
      { name: 'Authorization', type: 'string', required: true, desc: 'Bearer <token> (Dashboard user session)' },
    ],
    params: [
      { name: 'range', type: 'query string', required: false, desc: 'Time window for aggregation: "24h", "7d", or "30d". Default: 24h.' },
    ],
    responseSample: JSON.stringify(
      {
        range: '24h',
        sessions_count: 148,
        events_count: 892,
        errors_count: 4,
        error_free_pct: '97.3%',
        error_note: '4 unhandled exceptions',
        buckets: [
          { label: '00:00', sessions: 24, errors: 0 },
          { label: '06:00', sessions: 48, errors: 1 },
          { label: '12:00', sessions: 52, errors: 2 },
          { label: '18:00', sessions: 24, errors: 1 },
        ],
      },
      null,
      2,
    ),
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'get-events',
    title: 'List Telemetry Events',
    category: 'TELEMETRY QUERY API',
    method: 'GET',
    path: '/projects/:projectId/events?kind=all&limit=50',
    description:
      'Retrieves recent telemetry rows from the PostgreSQL partitioned table, ordered chronologically by event_timestamp DESC.',
    headers: [
      { name: 'Authorization', type: 'string', required: true, desc: 'Bearer <token>' },
    ],
    params: [
      { name: 'kind', type: 'query string', required: false, desc: 'Filter by item type: "all", "event", "log", or "session". Default: all.' },
      { name: 'limit', type: 'query integer', required: false, desc: 'Maximum records to return (1 - 100). Default: 50.' },
    ],
    responseSample: JSON.stringify(
      {
        items: [
          {
            id: '018f2d84-91f8-7440-a152-475968d9f4e2',
            kind: 'event',
            name: 'checkout_completed',
            session_id: 'ses_94829',
            event_timestamp: '2026-11-05T09:00:00Z',
            attributes: { amount_idr: 4500000, plan: 'enterprise' },
          },
        ],
      },
      null,
      2,
    ),
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'post-test-event',
    title: 'Trigger Test Event',
    category: 'TELEMETRY QUERY API',
    method: 'POST',
    path: '/projects/:projectId/test-event',
    description:
      'Enqueues a synthetic event into the live ingestion worker pool for instant verification of database writes and dashboard telemetry.',
    headers: [
      { name: 'Authorization', type: 'string', required: true, desc: 'Bearer <token>' },
    ],
    responseSample: JSON.stringify({ status: 'accepted', event_id: '018f2d84-91f8-7440-a152-475968d9f4e2' }, null, 2),
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'healthz',
    title: 'Health & Ingest Buffer Status',
    category: 'SYSTEM & HEALTH',
    method: 'GET',
    path: '/healthz',
    description:
      'Returns real-time health metrics including database pool readiness, queue buffer depth, active workers, and throughput statistics.',
    responseSample: JSON.stringify(
      {
        status: 'ok',
        db: 'up',
        ingest: {
          status: 'healthy',
          queue_depth: 0,
          queue_capacity: 2048,
          utilization_pct: 0,
          workers: 4,
          requests_per_sec: 14,
          events_received_total: 10420,
          events_flushed_total: 10420,
        },
      },
      null,
      2,
    ),
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'error-codes',
    title: 'Error Code Reference',
    category: 'SYSTEM & HEALTH',
    isGuide: true,
    description: 'Standardized HTTP status codes returned by ARTracker API endpoints.',
    guideContent: `### HTTP Status Codes

| Status | Code | Meaning | Remediation |
| :--- | :--- | :--- | :--- |
| **202** | \`Accepted\` | Payload validated and enqueued for async persistence | Normal operational response for ingest |
| **400** | \`Bad Request\` | Malformed JSON or validation rule failure | Check JSON syntax and ensure \`items\` array is not empty |
| **401** | \`Unauthorized\` | Invalid or missing client key | Provide a valid \`X-Artracker-Key\` header |
| **403** | \`Forbidden\` | Key revoked or insufficient project permissions | Re-issue API key in project Settings |
| **413** | \`Payload Too Large\` | Payload exceeded 512 KB compressed or 2 MB raw | Batch items into smaller chunks before dispatch |
| **429** | \`Too Many Requests\` | Per-project token bucket limit exceeded | Apply exponential backoff with jitter on SDK transport |
| **503** | \`Service Unavailable\` | Ingest buffer queue saturated | SDK should buffer events locally in memory/disk and retry |`,
  },
]

export default function DocsPage() {
  const { user } = useAuth()
  const [activeProjectId] = useState<number | null>(() => {
    try {
      const val = localStorage.getItem('artracker.project')
      return val ? Number(val) : null
    } catch {
      return null
    }
  })

  const [playgroundOpen, setPlaygroundOpen] = useState(true)
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')

  // Global keyboard shortcut: Cmd+K / Ctrl+K
  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen((prev) => !prev)
      } else if (e.key === 'Escape') {
        setSearchOpen(false)
      }
    }
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [])

  const filteredSections = useMemo(() => {
    if (!searchQuery.trim()) return SECTIONS
    const q = searchQuery.toLowerCase()
    return SECTIONS.filter(
      (s) =>
        s.title.toLowerCase().includes(q) ||
        s.category.toLowerCase().includes(q) ||
        (s.path && s.path.toLowerCase().includes(q)) ||
        s.description.toLowerCase().includes(q),
    )
  }, [searchQuery])

  return (
    <div className="fuma-root">
      {/* Top Navbar */}
      <header className="fuma-nav">
        <div className="fuma-nav__inner">
          <div style={{ display: 'flex', alignItems: 'center', gap: '1.25rem' }}>
            <Link to="/" className="fuma-brand">
              <svg style={{ width: 20, height: 20 }} viewBox="0 0 24 24" fill="none">
                <path d="M4 19L12 4L20 19M7.5 14H16.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="square" />
              </svg>
              <span>ARTracker</span>
            </Link>
            <span className="fuma-brand__badge">Docs v1.0</span>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            {/* Search Button (Fumadocs style) */}
            <button
              type="button"
              className="fuma-search-trigger"
              onClick={() => setSearchOpen(true)}
              aria-label="Search documentation"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
              </svg>
              <span>Search docs...</span>
              <kbd className="fuma-kbd">⌘K</kbd>
            </button>

            {/* Toggle Playground Button */}
            <button
              type="button"
              className={`fuma-panel-btn${playgroundOpen ? ' is-active' : ''}`}
              onClick={() => setPlaygroundOpen((prev) => !prev)}
              title={playgroundOpen ? 'Collapse API Playground' : 'Open API Playground'}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <rect width="18" height="18" x="3" y="3" rx="2" />
                <path d="M15 3v18" />
              </svg>
              <span>{playgroundOpen ? 'Hide Playground' : 'Open Playground'}</span>
            </button>

            <a
              href={`${BASE}/openapi.json`}
              target="_blank"
              rel="noreferrer"
              className="fuma-nav__link"
              style={{ fontSize: '0.8125rem', fontFamily: 'var(--font-outlier)' }}
            >
              OpenAPI Spec
            </a>

            {user && (
              <span style={{ fontSize: '0.8125rem', color: 'var(--color-muted)' }}>
                {user.email}
              </span>
            )}

            <Link to="/dashboard" className="dash-btn dash-btn--primary" style={{ padding: '0.35rem 0.75rem', fontSize: '0.8125rem' }}>
              Console Dashboard
            </Link>
          </div>
        </div>
      </header>

      {/* Main 3-Column Docs View */}
      <DocsView
        projectId={activeProjectId}
        playgroundOpen={playgroundOpen}
        onTogglePlayground={() => setPlaygroundOpen((prev) => !prev)}
        onSelect={() => setSearchOpen(false)}
      />

      {/* Command Palette / Search Modal */}
      {searchOpen && (
        <div
          className="fuma-modal-overlay"
          onClick={(e) => {
            if (e.target === e.currentTarget) setSearchOpen(false)
          }}
        >
          <div className="fuma-modal-card">
            <input
              type="text"
              autoFocus
              className="fuma-search-input"
              placeholder="Type a command or search documentation..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
            <div className="fuma-search-results">
              {filteredSections.length === 0 ? (
                <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--color-muted)', fontSize: '0.8125rem' }}>
                  No matching endpoints or guides found.
                </div>
              ) : (
                filteredSections.map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    className="fuma-search-item"
                    onClick={() => {
                      setSearchOpen(false)
                      const el = document.getElementById(`nav-${s.id}`)
                      el?.click()
                    }}
                  >
                    <div>
                      <div style={{ fontWeight: 600, fontSize: '0.875rem' }}>{s.title}</div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-muted)', marginTop: '0.15rem' }}>
                        {s.category} {s.path ? `· ${s.path}` : ''}
                      </div>
                    </div>
                    {s.method && (
                      <span className={`fuma-method fuma-method--${s.method.toLowerCase()}`}>
                        {s.method}
                      </span>
                    )}
                  </button>
                ))
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function DocsView({
  projectId,
  playgroundOpen,
  onTogglePlayground,
  onSelect,
}: {
  projectId?: number | null
  playgroundOpen: boolean
  onTogglePlayground: () => void
  onSelect?: () => void
}) {
  const [selectedId, setSelectedId] = useState<string>('ingest-batch')
  const [projectKeys, setProjectKeys] = useState<ApiKey[]>([])
  const [activeKey, setActiveKey] = useState<string>('')
  const [copiedUrl, setCopiedUrl] = useState(false)
  const [copiedCode, setCopiedCode] = useState(false)

  // Console / Playground state
  const [consoleMode, setConsoleMode] = useState<'playground' | 'code'>('playground')
  const [codeLang, setCodeLang] = useState<'curl' | 'node' | 'go' | 'python'>('curl')
  const [customHeaders, setCustomHeaders] = useState<string>('')
  const [requestBody, setRequestBody] = useState<string>('')
  const [useGzip, setUseGzip] = useState(false)
  const [executing, setExecuting] = useState(false)
  const [respStatus, setRespStatus] = useState<number | null>(null)
  const [respStatusText, setRespStatusText] = useState<string>('')
  const [respLatencyMs, setRespLatencyMs] = useState<number | null>(null)
  const [respBody, setRespBody] = useState<string>('')

  const activeSection = useMemo(
    () => SECTIONS.find((s) => s.id === selectedId) || SECTIONS[0],
    [selectedId],
  )

  // Fetch project keys if available
  useEffect(() => {
    if (!projectId) return
    keyApi
      .list(projectId)
      .then((res) => {
        setProjectKeys(res.items)
        if (res.items.length > 0) {
          setActiveKey(res.items[0].prefix)
        }
      })
      .catch((err) => console.error('Failed to load project keys', err))
  }, [projectId])

  // Sync section switch to playground
  useEffect(() => {
    const headers = { ...(activeSection.defaultHeaders || {}) }
    if (activeSection.headers?.some((h) => h.name === 'X-Artracker-Key') && activeKey) {
      headers['X-Artracker-Key'] = activeKey
    }
    setCustomHeaders(
      Object.entries(headers)
        .map(([k, v]) => `${k}: ${v}`)
        .join('\n'),
    )
    setRequestBody(activeSection.defaultBody || '')
    setRespStatus(null)
    setRespBody('')
    setRespLatencyMs(null)
  }, [activeSection, activeKey])

  // Resolved dynamic path
  const resolvedPath = useMemo(() => {
    if (!activeSection.path) return ''
    const pid = projectId ?? 1
    return activeSection.path.replace(':projectId', String(pid))
  }, [activeSection.path, projectId])

  const fullUrl = useMemo(() => {
    if (!resolvedPath) return ''
    const origin = typeof window !== 'undefined' ? window.location.origin : 'http://localhost:5174'
    if (resolvedPath === '/healthz') return `${origin}/healthz`
    return `${origin}${BASE}${resolvedPath}`
  }, [resolvedPath])

  // Execute playground request
  const handleExecute = async () => {
    if (!fullUrl || !activeSection.method) return
    setExecuting(true)
    setRespStatus(null)
    setRespBody('')
    setRespLatencyMs(null)

    const parsedHeaders: Record<string, string> = {}
    customHeaders.split('\n').forEach((line) => {
      const idx = line.indexOf(':')
      if (idx > 0) {
        const k = line.substring(0, idx).trim()
        const v = line.substring(idx + 1).trim()
        parsedHeaders[k] = v
      }
    })

    // Attach Bearer token if required
    if (activeSection.headers?.some((h) => h.name === 'Authorization')) {
      const token = getAccessToken()
      if (token && !parsedHeaders['Authorization']) {
        parsedHeaders['Authorization'] = `Bearer ${token}`
      }
    }

    let finalBody: BodyInit | undefined = undefined
    if (activeSection.method !== 'GET') {
      if (useGzip && typeof CompressionStream !== 'undefined') {
        try {
          const stream = new Blob([requestBody]).stream().pipeThrough(new CompressionStream('gzip'))
          finalBody = await new Response(stream).blob()
          parsedHeaders['Content-Encoding'] = 'gzip'
        } catch {
          finalBody = requestBody
        }
      } else {
        finalBody = requestBody
      }
    }

    const start = performance.now()
    try {
      const res = await fetch(fullUrl, {
        method: activeSection.method,
        headers: parsedHeaders,
        body: finalBody,
        credentials: 'include',
      })
      const latency = Math.round(performance.now() - start)
      setRespLatencyMs(latency)
      setRespStatus(res.status)
      setRespStatusText(res.statusText)

      const text = await res.text()
      try {
        const json = JSON.parse(text)
        setRespBody(JSON.stringify(json, null, 2))
      } catch {
        setRespBody(text || '(Empty 204 response)')
      }
    } catch (err: unknown) {
      const latency = Math.round(performance.now() - start)
      setRespLatencyMs(latency)
      setRespStatus(500)
      setRespStatusText('Network Connection Error')
      setRespBody(err instanceof Error ? err.message : String(err))
    } finally {
      setExecuting(false)
    }
  }

  // Generate code snippet
  const generatedCode = useMemo(() => {
    if (!fullUrl || !activeSection.method) return ''
    const method = activeSection.method
    const headersList = customHeaders
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean)

    if (codeLang === 'curl') {
      let cmd = `curl -X ${method} "${fullUrl}"`
      headersList.forEach((h) => {
        cmd += ` \\\n  -H "${h}"`
      })
      if (method !== 'GET' && requestBody) {
        if (useGzip) {
          cmd += ` \\\n  -H "Content-Encoding: gzip"`
          cmd += ` \\\n  --data-binary @- << 'EOF' | gzip -c\n${requestBody}\nEOF`
        } else {
          cmd += ` \\\n  -d '${requestBody.replace(/'/g, `'\\''`)}'`
        }
      }
      return cmd
    }

    if (codeLang === 'node') {
      const headersObj = headersList.reduce<Record<string, string>>((acc, l) => {
        const [k, ...rest] = l.split(':')
        acc[k.trim()] = rest.join(':').trim()
        return acc
      }, {})
      return `const response = await fetch("${fullUrl}", {
  method: "${method}",
  headers: ${JSON.stringify(headersObj, null, 4)},${
    method !== 'GET'
      ? `\n  body: JSON.stringify(${requestBody.replace(/\n/g, '\n  ')}),`
      : ''
  }
});

const data = await response.json();
console.log(response.status, data);`
    }

    if (codeLang === 'go') {
      return `package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func main() {
	url := "${fullUrl}"
	body := []byte(\`${requestBody}\`)

	req, _ := http.NewRequest("${method}", url, bytes.NewBuffer(body))
${headersList
  .map((h) => {
    const [k, ...v] = h.split(':')
    return `	req.Header.Set("${k.trim()}", "${v.join(':').trim()}")`
  })
  .join('\n')}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	resBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %d\\nResponse: %s\\n", resp.StatusCode, string(resBody))
}`
    }

    if (codeLang === 'python') {
      const headersObj = headersList.reduce<Record<string, string>>((acc, l) => {
        const [k, ...rest] = l.split(':')
        acc[k.trim()] = rest.join(':').trim()
        return acc
      }, {})
      return `import requests

url = "${fullUrl}"
headers = ${JSON.stringify(headersObj, null, 4)}
${
  method !== 'GET'
    ? `payload = ${requestBody.replace(/true/g, 'True').replace(/false/g, 'False').replace(/null/g, 'None')}

response = requests.${method.toLowerCase()}(url, json=payload, headers=headers)`
    : `response = requests.get(url, headers=headers)`
}

print(response.status_code, response.json())`
    }

    return ''
  }, [fullUrl, activeSection.method, customHeaders, requestBody, codeLang, useGzip])

  const copyUrl = () => {
    if (!fullUrl) return
    navigator.clipboard.writeText(fullUrl)
    setCopiedUrl(true)
    setTimeout(() => setCopiedUrl(false), 1600)
  }

  const copyCode = () => {
    navigator.clipboard.writeText(generatedCode)
    setCopiedCode(true)
    setTimeout(() => setCopiedCode(false), 1600)
  }

  // Categories list
  const categories = useMemo(() => {
    const list: string[] = []
    SECTIONS.forEach((s) => {
      if (!list.includes(s.category)) list.push(s.category)
    })
    return list
  }, [])

  return (
    <div className={`fuma-container${playgroundOpen ? '' : ' has-console-collapsed'}`}>
      {/* COLUMN 1: LEFT NAVIGATION */}
      <aside className="fuma-col-nav">
        {categories.map((cat) => (
          <div key={cat} className="fuma-group">
            <div className="fuma-group__title">{cat}</div>
            {SECTIONS.filter((s) => s.category === cat).map((s) => {
              const isActive = s.id === selectedId
              return (
                <button
                  id={`nav-${s.id}`}
                  key={s.id}
                  type="button"
                  className={`fuma-item${isActive ? ' is-active' : ''}`}
                  onClick={() => {
                    setSelectedId(s.id)
                    onSelect?.()
                  }}
                >
                  {s.method && (
                    <span className={`fuma-method fuma-method--${s.method.toLowerCase()}`}>
                      {s.method}
                    </span>
                  )}
                  <span>{s.title}</span>
                </button>
              )
            })}
          </div>
        ))}
      </aside>

      {/* COLUMN 2: CENTER DOCUMENTATION CONTENT */}
      <article className="fuma-col-content">
        <div className="fuma-crumbs">Docs &gt; {activeSection.category} &gt; {activeSection.title}</div>
        <h1 className="fuma-title">{activeSection.title}</h1>
        <p className="fuma-lead">{renderInline(activeSection.description)}</p>

        {/* Fumadocs Callout Cards */}
        {activeSection.callouts?.map((c) => (
          <div key={c.title} className={`fuma-callout fuma-callout--${c.type}`}>
            <strong>{renderInline(c.title)}</strong>
            <div>{renderInline(c.content)}</div>
          </div>
        ))}

        {activeSection.path && (
          <div className="fuma-endpoint-bar">
            <span className={`fuma-method fuma-method--${activeSection.method?.toLowerCase()}`}>
              {activeSection.method}
            </span>
            <span className="fuma-endpoint-bar__path">{fullUrl}</span>
            <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '0.375rem' }}>
              <button
                type="button"
                className="fuma-endpoint-bar__copy"
                onClick={copyUrl}
              >
                {copiedUrl ? 'Copied' : 'Copy'}
              </button>
              {!playgroundOpen && activeSection.method && (
                <button
                  type="button"
                  className="fuma-reopen-btn"
                  onClick={onTogglePlayground}
                  title="Open in Playground"
                >
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <polygon points="5 3 19 12 5 21 5 3" />
                  </svg>
                  <span>Test API</span>
                </button>
              )}
            </div>
          </div>
        )}

        {/* Guide / Text Sections */}
        {activeSection.isGuide && activeSection.guideContent && (
          <MarkdownContent content={activeSection.guideContent} />
        )}

        {/* Headers Table */}
        {activeSection.headers && activeSection.headers.length > 0 && (
          <section style={{ marginBottom: '2rem' }}>
            <h2 style={{ fontSize: '1rem', fontWeight: 600, margin: '0 0 0.75rem 0' }}>Request Headers</h2>
            <div className="fuma-table-wrap">
              <table className="fuma-table">
                <thead>
                  <tr>
                    <th>Header</th>
                    <th>Type</th>
                    <th>Description</th>
                  </tr>
                </thead>
                <tbody>
                  {activeSection.headers.map((h) => (
                    <tr key={h.name}>
                      <td>
                        <span className="fuma-param-name">{h.name}</span>
                        {h.required ? (
                          <span className="fuma-param-req">required</span>
                        ) : (
                          <span className="fuma-param-opt">optional</span>
                        )}
                      </td>
                      <td>
                        <span className="fuma-param-type">{h.type}</span>
                      </td>
                      <td>{renderInline(h.desc)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {/* Request Parameters Table */}
        {activeSection.params && activeSection.params.length > 0 && (
          <section style={{ marginBottom: '2rem' }}>
            <h2 style={{ fontSize: '1rem', fontWeight: 600, margin: '0 0 0.75rem 0' }}>Request Parameters</h2>
            <div className="fuma-table-wrap">
              <table className="fuma-table">
                <thead>
                  <tr>
                    <th>Parameter</th>
                    <th>Type</th>
                    <th>Description</th>
                  </tr>
                </thead>
                <tbody>
                  {activeSection.params.map((p) => (
                    <tr key={p.name}>
                      <td>
                        <span className="fuma-param-name">{p.name}</span>
                        {p.required ? (
                          <span className="fuma-param-req">required</span>
                        ) : (
                          <span className="fuma-param-opt">optional</span>
                        )}
                      </td>
                      <td>
                        <span className="fuma-param-type">{p.type}</span>
                      </td>
                      <td>
                        <div>{renderInline(p.desc)}</div>
                        {p.children && p.children.length > 0 && (
                          <div style={{ marginTop: '0.75rem' }}>
                            <table className="fuma-table" style={{ background: 'var(--color-paper-2)' }}>
                              <tbody>
                                {p.children.map((ch) => (
                                  <tr key={ch.name}>
                                    <td style={{ width: '40%' }}>
                                      <span className="fuma-param-name" style={{ fontSize: '0.75rem' }}>
                                        {ch.name}
                                      </span>
                                      {ch.required ? (
                                        <span className="fuma-param-req">required</span>
                                      ) : (
                                        <span className="fuma-param-opt">optional</span>
                                      )}
                                    </td>
                                    <td>
                                      <span className="fuma-param-type">{ch.type}</span>
                                      <div style={{ fontSize: '0.75rem', marginTop: '0.15rem' }}>
                                        {renderInline(ch.desc)}
                                      </div>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {/* Response Sample */}
        {activeSection.responseSample && (
          <section style={{ marginBottom: '2rem' }}>
            <h2 style={{ fontSize: '1rem', fontWeight: 600, margin: '0 0 0.75rem 0' }}>Response Sample</h2>
            <pre
              style={{
                background: 'var(--color-paper-2)',
                border: 'var(--rule-hair) solid var(--color-rule)',
                padding: '0.875rem',
                borderRadius: 'var(--radius-input)',
                fontSize: '0.8125rem',
                fontFamily: 'var(--font-outlier)',
                margin: 0,
                overflowX: 'auto',
              }}
            >
              {activeSection.responseSample}
            </pre>
          </section>
        )}
      </article>

      {/* COLUMN 3: RIGHT CONSOLE (DARK CHARCOAL) */}
      <aside className="fuma-col-right">
        {/* Header & Mode Switcher */}
        <div className="fuma-console-head">
          <div className="fuma-console-tabs">
            <button
              type="button"
              className={`fuma-tab-btn${consoleMode === 'playground' ? ' is-active' : ''}`}
              onClick={() => setConsoleMode('playground')}
            >
              Playground
            </button>
            <button
              type="button"
              className={`fuma-tab-btn${consoleMode === 'code' ? ' is-active' : ''}`}
              onClick={() => setConsoleMode('code')}
            >
              Code Snippets
            </button>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            {consoleMode === 'code' ? (
              <div className="fuma-console-tabs">
                {(['curl', 'node', 'go', 'python'] as const).map((lang) => (
                  <button
                    key={lang}
                    type="button"
                    className={`fuma-tab-btn${codeLang === lang ? ' is-active' : ''}`}
                    onClick={() => setCodeLang(lang)}
                    style={{ textTransform: 'uppercase', fontSize: '0.6875rem' }}
                  >
                    {lang === 'node' ? 'JS' : lang}
                  </button>
                ))}
              </div>
            ) : (
              <span style={{ fontSize: '0.6875rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                Live Client
              </span>
            )}

            <button
              type="button"
              className="fuma-close-btn"
              onClick={onTogglePlayground}
              title="Close Playground"
              aria-label="Close Playground"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M18 6 6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        {consoleMode === 'code' ? (
          /* Code Snippet View */
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
            <div className="fuma-editor-box" style={{ flex: 1 }}>
              <div className="fuma-editor-bar">
                <span>{codeLang.toUpperCase()} SDK Client</span>
                <button
                  type="button"
                  onClick={copyCode}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: '#94a3b8',
                    cursor: 'pointer',
                    fontSize: '0.6875rem',
                  }}
                >
                  {copiedCode ? 'Copied' : 'Copy'}
                </button>
              </div>
              <pre className="fuma-code-block" style={{ minHeight: 320 }}>
                {generatedCode}
              </pre>
            </div>
          </div>
        ) : (
          /* Interactive Playground View */
          <div style={{ display: 'flex', flexDirection: 'column', flex: 1 }}>
            {/* Presets and Key Configuration */}
            {activeSection.presets && activeSection.presets.length > 0 && (
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', margin: '0.5rem 0' }}>
                <span style={{ fontSize: '0.6875rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                  Payload Preset
                </span>
                <select
                  style={{
                    background: 'rgba(255,255,255,0.06)',
                    color: '#f8fafc',
                    border: '1px solid rgba(255,255,255,0.12)',
                    borderRadius: '4px',
                    padding: '0.25rem 0.5rem',
                    fontSize: '0.75rem',
                    outline: 'none',
                  }}
                  onChange={(e) => {
                    const pr = activeSection.presets?.find((x) => x.name === e.target.value)
                    if (pr) setRequestBody(pr.body)
                  }}
                >
                  {activeSection.presets.map((pr) => (
                    <option key={pr.name} value={pr.name} style={{ background: '#090d16', color: '#fff' }}>
                      {pr.name}
                    </option>
                  ))}
                </select>
              </div>
            )}

            {/* API Key Selector if applicable */}
            {activeSection.headers?.some((h) => h.name === 'X-Artracker-Key') && (
              <div style={{ margin: '0.5rem 0' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.25rem' }}>
                  <span style={{ fontSize: '0.6875rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                    X-Artracker-Key
                  </span>
                  {projectKeys.length > 0 && (
                    <span style={{ fontSize: '0.6875rem', color: '#64748b' }}>
                      Active Project #{projectId}
                    </span>
                  )}
                </div>
                <div
                  style={{
                    background: 'rgba(255,255,255,0.04)',
                    border: '1px solid rgba(255,255,255,0.08)',
                    borderRadius: '4px',
                    padding: '0.35rem 0.6rem',
                  }}
                >
                  <input
                    type="text"
                    placeholder="art_live_..."
                    style={{
                      background: 'transparent',
                      border: 'none',
                      color: '#f8fafc',
                      fontFamily: 'var(--font-outlier)',
                      fontSize: '0.75rem',
                      width: '100%',
                      outline: 'none',
                    }}
                    value={activeKey}
                    onChange={(e) => setActiveKey(e.target.value)}
                  />
                </div>
              </div>
            )}

            {/* Request Body / Query for endpoints */}
            {activeSection.method && activeSection.method !== 'GET' && (
              <div style={{ margin: '0.5rem 0' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.25rem' }}>
                  <span style={{ fontSize: '0.6875rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                    JSON Body
                  </span>
                  <label style={{ fontSize: '0.6875rem', color: '#94a3b8', display: 'flex', alignItems: 'center', gap: '0.25rem', cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={useGzip}
                      onChange={(e) => setUseGzip(e.target.checked)}
                    />
                    <span>Gzip Compress</span>
                  </label>
                </div>
                <div className="fuma-editor-box">
                  <textarea
                    rows={8}
                    className="fuma-textarea"
                    value={requestBody}
                    onChange={(e) => setRequestBody(e.target.value)}
                    placeholder="{ ... }"
                  />
                </div>
              </div>
            )}

            {/* Action Button */}
            {activeSection.method ? (
              <button
                type="button"
                className="fuma-run-btn"
                onClick={handleExecute}
                disabled={executing}
              >
                {executing ? 'Sending request...' : `Execute ${activeSection.method} Request`}
              </button>
            ) : (
              <div style={{ padding: '0.75rem', color: '#94a3b8', fontSize: '0.75rem', textAlign: 'center' }}>
                Select an API endpoint from the sidebar to execute live calls.
              </div>
            )}

            {/* Response Inspector */}
            <div className="fuma-resp-card" style={{ flex: 1 }}>
              <div className="fuma-resp-card__head">
                <span style={{ fontSize: '0.6875rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                  Response
                </span>
                {respStatus !== null && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                    <span
                      style={{
                        fontFamily: 'var(--font-outlier)',
                        fontSize: '0.6875rem',
                        fontWeight: 700,
                        padding: '0.125rem 0.4rem',
                        borderRadius: '3px',
                        background:
                          respStatus >= 200 && respStatus < 300
                            ? 'rgba(16, 185, 129, 0.2)'
                            : respStatus === 429
                              ? 'rgba(245, 158, 11, 0.2)'
                              : 'rgba(239, 68, 68, 0.2)',
                        color:
                          respStatus >= 200 && respStatus < 300
                            ? '#34d399'
                            : respStatus === 429
                              ? '#fbbf24'
                              : '#f87171',
                      }}
                    >
                      {respStatus} {respStatusText}
                    </span>
                    {respLatencyMs !== null && (
                      <span style={{ fontFamily: 'var(--font-outlier)', fontSize: '0.6875rem', color: '#94a3b8' }}>
                        {respLatencyMs} ms
                      </span>
                    )}
                  </div>
                )}
              </div>

              <pre className="fuma-resp-card__body">
                {respStatus === null && !executing
                  ? '// Response status and JSON output will appear here'
                  : executing
                    ? '// Transmitting...'
                    : respBody}
              </pre>
            </div>
          </div>
        )}
      </aside>
    </div>
  )
}
