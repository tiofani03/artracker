import { useEffect, useMemo, useState } from 'react'
import {
  BASE,
  getAccessToken,
  keyApi,
  type ApiKey,
  type Project,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import { Link } from '../router'
import '../dashboard.css'

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE'

type EndpointDef = {
  id: string
  name: string
  method: HttpMethod
  path: string
  category: 'Ingestion' | 'Telemetry Queries' | 'System & Health'
  description: string
  authType: 'api_key' | 'bearer' | 'none'
  defaultHeaders: Record<string, string>
  defaultBody?: string
  presets?: { name: string; desc: string; body: string }[]
}

const ENDPOINTS: EndpointDef[] = [
  {
    id: 'ingest-batch',
    name: 'Batch Telemetry Ingest',
    method: 'POST',
    path: '/ingest',
    category: 'Ingestion',
    description:
      'High-throughput asynchronous batch ingest for events, logs, and sessions. Non-blocking write with SLA <25ms. Supports optional gzip compression.',
    authType: 'api_key',
    defaultHeaders: {
      'Content-Type': 'application/json',
      'X-Artracker-Key': 'art_live_xxxxxxxxxxxxxxxx',
    },
    defaultBody: JSON.stringify(
      {
        sent_at: new Date().toISOString(),
        sdk: { name: 'artracker-browser', version: '1.2.0' },
        context: {
          environment: 'production',
          app_version: '2.4.1',
          user_agent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)',
        },
        items: [
          {
            type: 'event',
            name: 'campaign_attribution',
            session_id: 'ses_live_94829',
            user_id: 'usr_83910',
            timestamp: new Date().toISOString(),
            attributes: {
              utm_source: 'instagram',
              utm_medium: 'reels',
              utm_campaign: 'utm_launch_q4',
              referrer: 'https://instagram.com',
              landing_page: '/pricing',
            },
          },
          {
            type: 'event',
            name: 'checkout_completed',
            session_id: 'ses_live_94829',
            user_id: 'usr_83910',
            timestamp: new Date().toISOString(),
            attributes: {
              plan: 'enterprise_tier',
              billing_cycle: 'annual',
              amount_idr: 4500000,
              currency: 'IDR',
              items_count: 1,
            },
          },
        ],
      },
      null,
      2,
    ),
    presets: [
      {
        name: '🛒 Campaign Attribution & Purchase',
        desc: 'UTM marketing parameters linked to revenue conversion',
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
                user_id: 'customer.vip@commerce.co',
                timestamp: new Date().toISOString(),
                attributes: {
                  utm_source: 'google',
                  utm_medium: 'cpc',
                  utm_campaign: 'utm_launch_q4',
                  utm_term: 'realtime error tracker',
                  revenue: 1250000,
                },
              },
              {
                type: 'event',
                name: 'checkout_completed',
                session_id: `ses_${Date.now()}`,
                attributes: { order_id: 'ORD-9841', items: 3, total: 1250000 },
              },
            ],
          },
          null,
          2,
        ),
      },
      {
        name: '🚨 Unhandled Exception & Stack Trace',
        desc: 'Fatal client error log with component and device context',
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
        name: '🌐 Network 502 Bad Gateway Span',
        desc: 'HTTP API failure captured by client fetch interceptor',
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
                  url: 'https://api.payment-gateway.dev/v1/charge',
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
      {
        name: '📦 Multi-item Batch (Events & Logs)',
        desc: 'Standard SDK flush envelope containing 5 items',
        body: JSON.stringify(
          {
            sent_at: new Date().toISOString(),
            sdk: { name: 'artracker-browser', version: '1.2.0' },
            context: { environment: 'production', app_version: '2.4.1' },
            items: [
              {
                type: 'event',
                name: 'page_view',
                session_id: `ses_${Date.now()}`,
                timestamp: new Date().toISOString(),
                attributes: { path: '/dashboard/overview', referrer: 'https://google.com' },
              },
              {
                type: 'event',
                name: 'button_clicked',
                session_id: `ses_${Date.now()}`,
                attributes: { element_id: 'cta-primary', label: 'Start Free Trial' },
              },
              {
                type: 'log',
                level: 'warn',
                message: 'Template hydration took 2100ms exceeding 2s budget',
                session_id: `ses_${Date.now()}`,
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
    id: 'get-stats',
    name: 'Get Project Telemetry Stats',
    method: 'GET',
    path: '/projects/:projectId/stats?range=24h',
    category: 'Telemetry Queries',
    description:
      'Calculates aggregated telemetry metrics (sessions, total events, error counts, crash-free percentage, and time buckets) directly from PostgreSQL.',
    authType: 'bearer',
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'get-events',
    name: 'List Project Telemetry Events',
    method: 'GET',
    path: '/projects/:projectId/events?kind=all&limit=50',
    category: 'Telemetry Queries',
    description:
      'Returns raw telemetry rows from PostgreSQL telemetry_events partitioned table, order by event_timestamp DESC.',
    authType: 'bearer',
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'post-test-event',
    name: 'Trigger Real Test Telemetry Event',
    method: 'POST',
    path: '/projects/:projectId/test-event',
    category: 'Telemetry Queries',
    description:
      'Instantly enqueues 1 real test event into the high-performance async ingest queue and flushes it to PostgreSQL.',
    authType: 'bearer',
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'get-healthz',
    name: 'System Health & Ingest Buffer Depth',
    method: 'GET',
    path: '/healthz',
    category: 'System & Health',
    description:
      'Returns the real-time health of the database connection, worker pool status, channel queue depth, and batch buffer capacity.',
    authType: 'none',
    defaultHeaders: {
      Accept: 'application/json',
    },
  },
  {
    id: 'validate-key',
    name: 'Validate SDK Client Key',
    method: 'GET',
    path: '/sdk/validate',
    category: 'System & Health',
    description:
      'Ultra-fast in-memory lookup checking if a client API key is valid and retrieving its project ID without querying PostgreSQL.',
    authType: 'api_key',
    defaultHeaders: {
      'X-Artracker-Key': 'art_live_xxxxxxxxxxxxxxxx',
    },
  },
]

export default function DocsPage({ insideDashboard = false }: { insideDashboard?: boolean }) {
  if (insideDashboard) {
    return (
      <DashboardShell sectionTitle="API Docs & Playground" allowDemo>
        {({ project, isDemo }) => (
          <DocsInner
            project={project}
            isDemo={isDemo}
          />
        )}
      </DashboardShell>
    )
  }

  return (
    <div style={{ minHeight: '100vh', background: 'var(--color-paper)' }}>
      {/* Standalone Header */}
      <header
        style={{
          borderBottom: 'var(--rule-hair) solid var(--color-rule)',
          background: 'var(--color-paper-hi)',
          position: 'sticky',
          top: 0,
          zIndex: 100,
        }}
      >
        <div
          style={{
            maxWidth: 1360,
            margin: '0 auto',
            padding: '0.875rem 1.5rem',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
            <Link
              to="/"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.5rem',
                textDecoration: 'none',
                color: 'var(--color-ink)',
                fontWeight: 700,
                fontSize: '1.125rem',
              }}
            >
              <svg style={{ width: 22, height: 22 }} viewBox="0 0 24 24" fill="none">
                <path
                  d="M4 19L12 4L20 19M7.5 14H16.5"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="square"
                />
              </svg>
              <span>ARTracker</span>
            </Link>
            <span
              style={{
                fontSize: '0.75rem',
                padding: '0.15rem 0.5rem',
                borderRadius: '4px',
                background: 'var(--color-accent-subtle)',
                color: 'var(--color-accent)',
                fontWeight: 600,
              }}
            >
              API Reference v1
            </span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <Link to="/dashboard" className="dash-btn dash-btn--primary">
              Console Dashboard →
            </Link>
          </div>
        </div>
      </header>

      <main style={{ maxWidth: 1360, margin: '0 auto', padding: '2rem 1.5rem' }}>
        <DocsInner />
      </main>
    </div>
  )
}

function DocsInner({
  project,
  isDemo,
}: {
  project?: Project | null
  isDemo?: boolean
}) {
  const [selectedEndpoint, setSelectedEndpoint] = useState<EndpointDef>(ENDPOINTS[0])
  const [projectKeys, setProjectKeys] = useState<ApiKey[]>([])
  const [activeKey, setActiveKey] = useState<string>('')
  const [customHeaders, setCustomHeaders] = useState<string>('')
  const [requestBody, setRequestBody] = useState<string>(selectedEndpoint.defaultBody || '')
  const [useGzip, setUseGzip] = useState(false)
  const [codeLang, setCodeLang] = useState<'curl' | 'js' | 'go' | 'python'>('curl')
  const [activeTab, setActiveTab] = useState<'playground' | 'spec' | 'architecture'>('playground')

  // Execution state
  const [executing, setExecuting] = useState(false)
  const [respStatus, setRespStatus] = useState<number | null>(null)
  const [respStatusText, setRespStatusText] = useState<string>('')
  const [respLatencyMs, setRespLatencyMs] = useState<number | null>(null)
  const [respHeaders, setRespHeaders] = useState<Record<string, string>>({})
  const [respBody, setRespBody] = useState<string>('')
  const [copiedCode, setCopiedCode] = useState(false)
  const [copiedKey, setCopiedKey] = useState(false)

  // Fetch project API keys when inside a valid project
  useEffect(() => {
    if (!project || isDemo) return
    keyApi
      .list(project.id)
      .then((res) => {
        setProjectKeys(res.items)
        if (res.items.length > 0) {
          setActiveKey(res.items[0].prefix)
        }
      })
      .catch((err) => console.error('Failed to load project keys', err))
  }, [project?.id, isDemo])

  // Sync endpoint changes to headers & body
  useEffect(() => {
    const headers = { ...selectedEndpoint.defaultHeaders }
    if (selectedEndpoint.authType === 'api_key' && activeKey) {
      headers['X-Artracker-Key'] = activeKey
    }
    setCustomHeaders(
      Object.entries(headers)
        .map(([k, v]) => `${k}: ${v}`)
        .join('\n'),
    )
    setRequestBody(selectedEndpoint.defaultBody || '')
    setRespStatus(null)
    setRespBody('')
    setRespLatencyMs(null)
  }, [selectedEndpoint, activeKey])

  // Resolved dynamic path replacing `:projectId`
  const resolvedPath = useMemo(() => {
    let p = selectedEndpoint.path
    const pid = project?.id ?? 1
    p = p.replace(':projectId', String(pid))
    return p
  }, [selectedEndpoint.path, project?.id])

  const fullUrl = useMemo(() => {
    const origin = typeof window !== 'undefined' ? window.location.origin : 'http://localhost:5174'
    if (resolvedPath === '/healthz') return `${origin}/healthz`
    return `${origin}${BASE}${resolvedPath}`
  }, [resolvedPath])

  // Execute request
  const handleExecute = async () => {
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

    // Auto-attach Bearer token if required
    if (selectedEndpoint.authType === 'bearer') {
      const token = getAccessToken()
      if (token && !parsedHeaders['Authorization']) {
        parsedHeaders['Authorization'] = `Bearer ${token}`
      }
    }

    let finalBody: BodyInit | undefined = undefined
    if (selectedEndpoint.method !== 'GET') {
      if (useGzip && typeof CompressionStream !== 'undefined') {
        try {
          const stream = new Blob([requestBody])
            .stream()
            .pipeThrough(new CompressionStream('gzip'))
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
        method: selectedEndpoint.method,
        headers: parsedHeaders,
        body: finalBody,
        credentials: 'include',
      })
      const latency = Math.round(performance.now() - start)
      setRespLatencyMs(latency)
      setRespStatus(res.status)
      setRespStatusText(res.statusText)

      const hdrs: Record<string, string> = {}
      res.headers.forEach((val, key) => {
        hdrs[key] = val
      })
      setRespHeaders(hdrs)

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
      setRespStatusText('Client Network Error')
      setRespBody(err instanceof Error ? err.message : String(err))
    } finally {
      setExecuting(false)
    }
  }

  // Generate code snippet
  const generatedCode = useMemo(() => {
    const method = selectedEndpoint.method
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

    if (codeLang === 'js') {
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
${headersList.map((h) => {
  const [k, ...v] = h.split(':')
  return `	req.Header.Set("${k.trim()}", "${v.join(':').trim()}")`
}).join('\n')}

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
  }, [selectedEndpoint, customHeaders, requestBody, fullUrl, codeLang, useGzip])

  const copyCodeToClipboard = () => {
    navigator.clipboard.writeText(generatedCode)
    setCopiedCode(true)
    setTimeout(() => setCopiedCode(false), 1800)
  }

  const copyKeyToClipboard = (k: string) => {
    navigator.clipboard.writeText(k)
    setCopiedKey(true)
    setTimeout(() => setCopiedKey(false), 1800)
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      {/* Title & Context */}
      <div className="dash-title" style={{ marginBottom: 0 }}>
        <div>
          <h1 style={{ display: 'flex', alignItems: 'center', gap: '0.625rem' }}>
            <span>Backend Documentation &amp; API Playground</span>
            <span
              style={{
                fontSize: '0.75rem',
                padding: '0.2rem 0.5rem',
                borderRadius: '4px',
                background: 'var(--color-accent)',
                color: '#fff',
                fontWeight: 600,
                letterSpacing: '0.02em',
              }}
            >
              Enterprise Tier
            </span>
          </h1>
          <p>
            Interactive testing harness, envelope specifications, and client SDK integration guide
            for ARTracker high-throughput ingest pipeline.
          </p>
        </div>
      </div>

      {/* Navigation Tabs */}
      <div
        className="dash-seg"
        style={{ alignSelf: 'flex-start' }}
        role="tablist"
        aria-label="Documentation sections"
      >
        <button
          type="button"
          className={`dash-seg__btn${activeTab === 'playground' ? ' is-active' : ''}`}
          onClick={() => setActiveTab('playground')}
        >
          ⚡ Interactive Playground
        </button>
        <button
          type="button"
          className={`dash-seg__btn${activeTab === 'spec' ? ' is-active' : ''}`}
          onClick={() => setActiveTab('spec')}
        >
          📖 Ingest Envelope Spec
        </button>
        <button
          type="button"
          className={`dash-seg__btn${activeTab === 'architecture' ? ' is-active' : ''}`}
          onClick={() => setActiveTab('architecture')}
        >
          🏛️ High-Perf Architecture (&lt;25ms SLA)
        </button>
      </div>

      {/* ========================================================= */}
      {/* SECTION 1: INTERACTIVE PLAYGROUND                         */}
      {/* ========================================================= */}
      {activeTab === 'playground' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
          {/* Active Project & API Key Strip */}
          {project && (
            <div
              className="dash-card dash-card--compact"
              style={{
                display: 'flex',
                alignItems: 'center',
                flexWrap: 'wrap',
                gap: '1rem',
                background: 'var(--color-paper-2)',
                borderColor: 'var(--color-rule)',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <span style={{ fontSize: '0.8125rem', color: 'var(--color-fg-muted)' }}>
                  Active Project:
                </span>
                <strong style={{ fontSize: '0.875rem' }}>{project.name}</strong>
                <span className="badge badge--role">ID: {project.id}</span>
              </div>

              {projectKeys.length > 0 ? (
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginLeft: 'auto' }}>
                  <span style={{ fontSize: '0.8125rem', color: 'var(--color-fg-muted)' }}>
                    Using API Key:
                  </span>
                  <select
                    className="dash-select"
                    value={activeKey}
                    onChange={(e) => setActiveKey(e.target.value)}
                    style={{ fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}
                  >
                    {projectKeys.map((k) => (
                      <option key={k.id} value={k.prefix}>
                        {k.key_type} key ({k.prefix})
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    className="dash-btn"
                    onClick={() => copyKeyToClipboard(activeKey)}
                    style={{ padding: '0.25rem 0.5rem', fontSize: '0.75rem' }}
                  >
                    {copiedKey ? '✓ Copied' : 'Copy'}
                  </button>
                </div>
              ) : (
                <div style={{ marginLeft: 'auto', fontSize: '0.8125rem', color: 'var(--color-fg-muted)' }}>
                  No API keys generated yet.{' '}
                  <Link to="/dashboard/settings" style={{ color: 'var(--color-accent)' }}>
                    Create one in Settings →
                  </Link>
                </div>
              )}
            </div>
          )}

          {/* Playground Main Layout: Left Request Builder, Right Live Execution */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit, minmax(460px, 1fr))',
              gap: '1.25rem',
            }}
          >
            {/* LEFT COLUMN: Request Builder */}
            <div className="dash-card dash-card--compact" style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div className="dash-card__head" style={{ marginBottom: 0 }}>
                <div>
                  <h2 className="dash-card__title">Request Builder</h2>
                  <p className="dash-card__desc">Choose an endpoint and configure payload &amp; headers</p>
                </div>
              </div>

              {/* Endpoint Selector */}
              <div>
                <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-fg-muted)', marginBottom: '0.25rem' }}>
                  Target Endpoint
                </label>
                <select
                  className="dash-select"
                  style={{ width: '100%', fontFamily: 'var(--font-mono)', fontSize: '0.875rem' }}
                  value={selectedEndpoint.id}
                  onChange={(e) => {
                    const ep = ENDPOINTS.find((x) => x.id === e.target.value)
                    if (ep) setSelectedEndpoint(ep)
                  }}
                >
                  <optgroup label="Ingestion">
                    {ENDPOINTS.filter((e) => e.category === 'Ingestion').map((e) => (
                      <option key={e.id} value={e.id}>
                        {e.method} {e.path} — {e.name}
                      </option>
                    ))}
                  </optgroup>
                  <optgroup label="Telemetry Queries">
                    {ENDPOINTS.filter((e) => e.category === 'Telemetry Queries').map((e) => (
                      <option key={e.id} value={e.id}>
                        {e.method} {e.path} — {e.name}
                      </option>
                    ))}
                  </optgroup>
                  <optgroup label="System & Health">
                    {ENDPOINTS.filter((e) => e.category === 'System & Health').map((e) => (
                      <option key={e.id} value={e.id}>
                        {e.method} {e.path} — {e.name}
                      </option>
                    ))}
                  </optgroup>
                </select>
              </div>

              {/* URL Preview */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.5rem',
                  padding: '0.5rem 0.75rem',
                  background: 'var(--color-paper-2)',
                  borderRadius: 'var(--cut-m)',
                  border: 'var(--rule-hair) solid var(--color-rule)',
                  fontFamily: 'var(--font-mono)',
                  fontSize: '0.8125rem',
                }}
              >
                <span
                  style={{
                    padding: '0.15rem 0.4rem',
                    borderRadius: '3px',
                    fontWeight: 700,
                    fontSize: '0.75rem',
                    background:
                      selectedEndpoint.method === 'POST'
                        ? 'oklch(45% 0.18 142 / 0.15)'
                        : 'oklch(52% 0.22 250 / 0.15)',
                    color:
                      selectedEndpoint.method === 'POST'
                        ? 'oklch(45% 0.18 142)'
                        : 'oklch(52% 0.22 250)',
                  }}
                >
                  {selectedEndpoint.method}
                </span>
                <span style={{ wordBreak: 'break-all', color: 'var(--color-fg)' }}>
                  {fullUrl}
                </span>
              </div>

              <p style={{ margin: 0, fontSize: '0.8125rem', color: 'var(--color-fg-muted)' }}>
                {selectedEndpoint.description}
              </p>

              {/* Presets if available */}
              {selectedEndpoint.presets && selectedEndpoint.presets.length > 0 && (
                <div>
                  <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-fg-muted)', marginBottom: '0.375rem' }}>
                    Quick Presets (1-Click Load)
                  </label>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.375rem' }}>
                    {selectedEndpoint.presets.map((pr) => (
                      <button
                        key={pr.name}
                        type="button"
                        className="dash-btn"
                        style={{ fontSize: '0.75rem', padding: '0.25rem 0.5rem' }}
                        onClick={() => setRequestBody(pr.body)}
                        title={pr.desc}
                      >
                        {pr.name}
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {/* Headers */}
              <div>
                <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-fg-muted)', marginBottom: '0.25rem' }}>
                  Request Headers (key: value)
                </label>
                <textarea
                  rows={selectedEndpoint.method === 'GET' ? 3 : 4}
                  className="dash-input"
                  style={{
                    width: '100%',
                    fontFamily: 'var(--font-mono)',
                    fontSize: '0.75rem',
                    resize: 'vertical',
                    padding: '0.5rem',
                    lineHeight: 1.4,
                  }}
                  value={customHeaders}
                  onChange={(e) => setCustomHeaders(e.target.value)}
                />
              </div>

              {/* Body Editor (for POST) */}
              {selectedEndpoint.method !== 'GET' && (
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.25rem' }}>
                    <label style={{ fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-fg-muted)' }}>
                      JSON Request Body
                    </label>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                      <label style={{ fontSize: '0.75rem', display: 'flex', alignItems: 'center', gap: '0.25rem', cursor: 'pointer' }}>
                        <input
                          type="checkbox"
                          checked={useGzip}
                          onChange={(e) => setUseGzip(e.target.checked)}
                        />
                        <span>Gzip Encoded</span>
                      </label>
                      <button
                        type="button"
                        className="dash-btn"
                        style={{ padding: '0.15rem 0.4rem', fontSize: '0.7rem' }}
                        onClick={() => {
                          try {
                            setRequestBody(JSON.stringify(JSON.parse(requestBody), null, 2))
                          } catch {
                            // ignore invalid format
                          }
                        }}
                      >
                        Format
                      </button>
                    </div>
                  </div>
                  <textarea
                    rows={12}
                    className="dash-input"
                    style={{
                      width: '100%',
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      resize: 'vertical',
                      padding: '0.625rem',
                      lineHeight: 1.4,
                      background: 'var(--color-paper-hi)',
                    }}
                    value={requestBody}
                    onChange={(e) => setRequestBody(e.target.value)}
                  />
                </div>
              )}

              {/* Action Button */}
              <button
                type="button"
                className="dash-btn dash-btn--primary"
                onClick={handleExecute}
                disabled={executing}
                style={{
                  padding: '0.75rem 1.25rem',
                  fontSize: '0.875rem',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: '0.5rem',
                }}
              >
                {executing ? (
                  <>
                    <span className="dash-spinner" /> Executing Request...
                  </>
                ) : (
                  <>⚡ Send Request to Backend</>
                )}
              </button>
            </div>

            {/* RIGHT COLUMN: Response Viewer & Multi-Language SDK Snippets */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
              {/* Response Inspector Card */}
              <section className="dash-card dash-card--compact" style={{ flex: 1 }}>
                <div className="dash-card__head">
                  <div>
                    <h2 className="dash-card__title">Real-Time Response Inspector</h2>
                    <p className="dash-card__desc">Live round-trip status, latency, and response headers</p>
                  </div>
                  {respStatus !== null && (
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span
                        className={`badge ${
                          respStatus >= 200 && respStatus < 300
                            ? 'badge--active'
                            : respStatus === 429
                              ? 'badge--console'
                              : 'badge--exception'
                        }`}
                        style={{ fontWeight: 700 }}
                      >
                        {respStatus} {respStatusText}
                      </span>
                      {respLatencyMs !== null && (
                        <span
                          className="dash-code"
                          style={{
                            fontSize: '0.75rem',
                            color: respLatencyMs < 25 ? 'oklch(45% 0.18 142)' : 'inherit',
                          }}
                        >
                          ⚡ {respLatencyMs} ms {respLatencyMs < 25 ? '(SLA OK)' : ''}
                        </span>
                      )}
                    </div>
                  )}
                </div>

                {respStatus === null && !executing && (
                  <div
                    style={{
                      padding: '4rem 1rem',
                      textAlign: 'center',
                      color: 'var(--color-fg-muted)',
                      border: '1px dashed var(--color-rule)',
                      borderRadius: 'var(--cut-m)',
                    }}
                  >
                    <div style={{ fontSize: '1.75rem', marginBottom: '0.5rem' }}>📡</div>
                    <p style={{ margin: '0 0 0.25rem 0', fontWeight: 500, color: 'var(--color-fg)' }}>
                      Ready to transmit
                    </p>
                    <p style={{ margin: 0, fontSize: '0.8125rem' }}>
                      Click <strong>Send Request to Backend</strong> to test this endpoint live.
                    </p>
                  </div>
                )}

                {executing && (
                  <div style={{ padding: '3.5rem 1rem', textAlign: 'center', color: 'var(--color-fg-muted)' }}>
                    <div style={{ fontSize: '1.5rem', marginBottom: '0.5rem' }}>⏳</div>
                    <p style={{ margin: 0, fontSize: '0.875rem' }}>Connecting to API...</p>
                  </div>
                )}

                {respStatus !== null && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                    <div
                      style={{
                        position: 'relative',
                        background: 'var(--color-paper-2)',
                        border: 'var(--rule-hair) solid var(--color-rule)',
                        borderRadius: 'var(--cut-m)',
                        padding: '0.75rem',
                        overflowX: 'auto',
                      }}
                    >
                      <pre
                        style={{
                          margin: 0,
                          fontSize: '0.75rem',
                          fontFamily: 'var(--font-mono)',
                          whiteSpace: 'pre-wrap',
                          wordBreak: 'break-all',
                          lineHeight: 1.45,
                        }}
                      >
                        {respBody}
                      </pre>
                    </div>

                    {Object.keys(respHeaders).length > 0 && (
                      <details style={{ fontSize: '0.75rem' }}>
                        <summary style={{ cursor: 'pointer', color: 'var(--color-fg-muted)', userSelect: 'none' }}>
                          View Response Headers ({Object.keys(respHeaders).length})
                        </summary>
                        <div
                          style={{
                            marginTop: '0.5rem',
                            padding: '0.5rem',
                            background: 'var(--color-paper-2)',
                            borderRadius: 'var(--cut-s)',
                            fontFamily: 'var(--font-mono)',
                            display: 'grid',
                            gridTemplateColumns: 'max-content 1fr',
                            gap: '0.25rem 0.75rem',
                          }}
                        >
                          {Object.entries(respHeaders).map(([k, v]) => (
                            <div key={k} style={{ display: 'contents' }}>
                              <span style={{ color: 'var(--color-fg-muted)' }}>{k}:</span>
                              <span>{v}</span>
                            </div>
                          ))}
                        </div>
                      </details>
                    )}
                  </div>
                )}
              </section>

              {/* Code Snippet Generator Card */}
              <section className="dash-card dash-card--compact">
                <div className="dash-card__head" style={{ marginBottom: '0.5rem' }}>
                  <div>
                    <h2 className="dash-card__title">Code Snippet Generator</h2>
                    <p className="dash-card__desc">Ready-to-use production SDK and CLI commands</p>
                  </div>
                  <div className="dash-seg">
                    {(['curl', 'js', 'go', 'python'] as const).map((lang) => (
                      <button
                        key={lang}
                        type="button"
                        className={`dash-seg__btn${codeLang === lang ? ' is-active' : ''}`}
                        onClick={() => setCodeLang(lang)}
                        style={{ textTransform: 'uppercase', fontSize: '0.7rem' }}
                      >
                        {lang}
                      </button>
                    ))}
                  </div>
                </div>

                <div
                  style={{
                    position: 'relative',
                    background: 'var(--color-ink)',
                    color: '#e2e8f0',
                    borderRadius: 'var(--cut-m)',
                    padding: '0.875rem',
                    overflowX: 'auto',
                  }}
                >
                  <button
                    type="button"
                    onClick={copyCodeToClipboard}
                    style={{
                      position: 'absolute',
                      top: '0.5rem',
                      right: '0.5rem',
                      padding: '0.25rem 0.5rem',
                      background: 'rgba(255,255,255,0.15)',
                      border: 'none',
                      borderRadius: '4px',
                      color: '#fff',
                      fontSize: '0.75rem',
                      cursor: 'pointer',
                    }}
                  >
                    {copiedCode ? '✓ Copied' : 'Copy'}
                  </button>
                  <pre
                    style={{
                      margin: 0,
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      lineHeight: 1.45,
                      whiteSpace: 'pre',
                    }}
                  >
                    {generatedCode}
                  </pre>
                </div>
              </section>
            </div>
          </div>
        </div>
      )}

      {/* ========================================================= */}
      {/* SECTION 2: ENVELOPE SPECIFICATION                         */}
      {/* ========================================================= */}
      {activeTab === 'spec' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
          <section className="dash-card">
            <h2 className="dash-card__title">Ingest Envelope Specification (`POST /api/v1/ingest`)</h2>
            <p className="dash-card__desc">
              Every client telemetry payload is transmitted as an Envelope containing top-level metadata and an array of telemetry items.
            </p>

            <div className="dash-table-wrap" style={{ marginTop: '1rem' }}>
              <table className="dash-table">
                <thead>
                  <tr>
                    <th>Field</th>
                    <th>Type</th>
                    <th>Required</th>
                    <th>Description</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td className="dash-code">sent_at</td>
                    <td>ISO-8601 String</td>
                    <td>Optional</td>
                    <td>Client device wall-clock time when envelope was dispatched. Used to calculate clock-skew.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">sdk.name</td>
                    <td>String</td>
                    <td>Optional</td>
                    <td>Client SDK identifier (e.g. <code>artracker-browser</code>, <code>artracker-android</code>).</td>
                  </tr>
                  <tr>
                    <td className="dash-code">sdk.version</td>
                    <td>String</td>
                    <td>Optional</td>
                    <td>Semantic version string of the client library (e.g. <code>1.2.0</code>).</td>
                  </tr>
                  <tr>
                    <td className="dash-code">context</td>
                    <td>JSON Object</td>
                    <td>Optional</td>
                    <td>
                      Global environment metadata: <code>environment</code> (default: <code>production</code>),
                      <code>release_version</code>, <code>os</code>, <code>browser</code>.
                    </td>
                  </tr>
                  <tr>
                    <td className="dash-code">items</td>
                    <td>Array&lt;RawItem&gt;</td>
                    <td><strong>Yes</strong></td>
                    <td>Array of 1 to 500 telemetry items (events, logs, or sessions).</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section className="dash-card">
            <h2 className="dash-card__title">Item Schema (`RawItem`)</h2>
            <p className="dash-card__desc">
              Structure for each individual event or error inside the <code>items</code> array.
            </p>

            <div className="dash-table-wrap" style={{ marginTop: '1rem' }}>
              <table className="dash-table">
                <thead>
                  <tr>
                    <th>Field</th>
                    <th>Type</th>
                    <th>Allowed Values</th>
                    <th>Description</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td className="dash-code">type</td>
                    <td>String</td>
                    <td><code>event</code> | <code>log</code> | <code>session</code></td>
                    <td>Telemetry classification. Default: <code>event</code>.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">name</td>
                    <td>String</td>
                    <td>Max 255 chars</td>
                    <td>Event identifier (e.g. <code>checkout_completed</code>, <code>campaign_attribution</code>).</td>
                  </tr>
                  <tr>
                    <td className="dash-code">level</td>
                    <td>String</td>
                    <td><code>debug</code> | <code>info</code> | <code>warn</code> | <code>error</code> | <code>fatal</code></td>
                    <td>Severity level for logs/exceptions. Required when <code>type = "log"</code>.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">message</td>
                    <td>String</td>
                    <td>Max 4,096 chars</td>
                    <td>Human-readable error message or log detail.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">session_id</td>
                    <td>String</td>
                    <td>Max 128 chars</td>
                    <td>Unique browser session token linking events directly to Session Replays.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">user_id</td>
                    <td>String</td>
                    <td>Max 128 chars</td>
                    <td>Pseudonymous user identifier or email.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">timestamp</td>
                    <td>ISO-8601 String</td>
                    <td>UTC timestamp</td>
                    <td>When the event occurred on the client. Normalized against clock drift.</td>
                  </tr>
                  <tr>
                    <td className="dash-code">attributes</td>
                    <td>JSON Object</td>
                    <td>Max 32 KB</td>
                    <td>Flexible JSON dictionary for UTM tags, custom telemetry, and business metrics.</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section className="dash-card">
            <h2 className="dash-card__title">Hard Quotas &amp; Ingress Limits</h2>
            <div className="dash-table-wrap" style={{ marginTop: '1rem' }}>
              <table className="dash-table">
                <thead>
                  <tr>
                    <th>Parameter</th>
                    <th>Threshold</th>
                    <th>Enforcement Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>Max Compressed Request Body</td>
                    <td className="dash-code">512 KB</td>
                    <td>HTTP 413 Payload Too Large</td>
                  </tr>
                  <tr>
                    <td>Max Decompressed Payload</td>
                    <td className="dash-code">2.0 MB</td>
                    <td>Anti-Zipbomb LimitReader terminates stream</td>
                  </tr>
                  <tr>
                    <td>Max Items per Batch</td>
                    <td className="dash-code">500 items</td>
                    <td>HTTP 400 Bad Request</td>
                  </tr>
                  <tr>
                    <td>Max Attributes Size per Item</td>
                    <td className="dash-code">32 KB</td>
                    <td>Truncated with <code>__truncated: true</code> marker</td>
                  </tr>
                  <tr>
                    <td>Rate Limit (Default)</td>
                    <td className="dash-code">120 RPS / 1,200 EPS</td>
                    <td>HTTP 429 Too Many Requests (Token Bucket)</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div>
      )}

      {/* ========================================================= */}
      {/* SECTION 3: ARCHITECTURE OVERVIEW                          */}
      {/* ========================================================= */}
      {activeTab === 'architecture' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
          <section className="dash-card">
            <h2 className="dash-card__title">Pipeline Architecture (&lt;25ms Non-Blocking Write SLA)</h2>
            <p className="dash-card__desc">
              How ARTracker achieves extreme ingestion throughput for 1,000,000+ daily active users without dropping telemetry.
            </p>

            <div
              style={{
                marginTop: '1.25rem',
                padding: '1.5rem',
                background: 'var(--color-paper-2)',
                borderRadius: 'var(--cut-m)',
                border: 'var(--rule-hair) solid var(--color-rule)',
                fontFamily: 'var(--font-mono)',
                fontSize: '0.8125rem',
                lineHeight: 1.6,
                overflowX: 'auto',
              }}
            >
              <pre style={{ margin: 0 }}>
{`[Client SDK] ──(HTTP/2 + Gzip)──> [Go Fiber Ingest API]
                                         │
               ┌─────────────────────────┴─────────────────────────┐
               │ 1. Header & Magic Bytes Check                     │
               │ 2. Sharded LRU Key Cache (<0.1ms lookup)          │
               │ 3. Per-Project Token Bucket Rate Limiter          │
               │ 4. sync.Pool Gzip Decompressor (Zero-Alloc)       │
               │ 5. Enqueue to Buffered Channel                    │
               └─────────────────────────┬─────────────────────────┘
                                         │
                    HTTP 202 Accepted (<15ms SLA achieved!)
                                         │
                                         ▼
                            [Async Ingest Worker Pool]
                                         │
                   ┌─────────────────────┴─────────────────────┐
                   │ • Schema sanitization & attribute capping │
                   │ • Clock-skew timestamp normalization      │
                   │ • UUIDv7 time-ordered ID generation       │
                   └─────────────────────┬─────────────────────┘
                                         │
                     Dual Trigger: N=500 items OR T=250ms
                                         │
                                         ▼
                     [Binary pgx.CopyFrom Bulk Protocol]
                                         │
                                         ▼
                 [PostgreSQL Partitioned Table: telemetry_events]`}
              </pre>
            </div>
          </section>

          <section className="dash-card">
            <h2 className="dash-card__title">HTTP Status Code Reference</h2>
            <div className="dash-table-wrap" style={{ marginTop: '1rem' }}>
              <table className="dash-table">
                <thead>
                  <tr>
                    <th>Status</th>
                    <th>Title</th>
                    <th>Meaning &amp; Client Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td><span className="badge badge--active">202 Accepted</span></td>
                    <td>Enqueued Successfully</td>
                    <td>Payload passed validation and is queued for bulk persistence. SLA &lt;25ms.</td>
                  </tr>
                  <tr>
                    <td><span className="badge badge--exception">400 Bad Request</span></td>
                    <td>Malformed JSON / Empty Batch</td>
                    <td>Envelope contains syntax error or zero items. Check payload formatting.</td>
                  </tr>
                  <tr>
                    <td><span className="badge badge--exception">401 Unauthorized</span></td>
                    <td>Invalid API Key</td>
                    <td>Missing or unrecognized <code>X-Artracker-Key</code> header.</td>
                  </tr>
                  <tr>
                    <td><span className="badge badge--exception">403 Forbidden</span></td>
                    <td>Revoked Key</td>
                    <td>API key has been revoked by workspace admin in Settings.</td>
                  </tr>
                  <tr>
                    <td><span className="badge badge--exception">413 Payload Too Large</span></td>
                    <td>Exceeded 512KB gzip / 2MB raw</td>
                    <td>Batch envelope exceeds maximum size limits. Split into smaller batches.</td>
                  </tr>
                  <tr>
                    <td><span className="badge badge--console">429 Too Many Requests</span></td>
                    <td>Rate Limited</td>
                    <td>Exceeded project RPS or EPS tokens. Backoff using exponential jitter.</td>
                  </tr>
                  <tr>
                    <td><span className="badge badge--exception">503 Service Unavailable</span></td>
                    <td>Queue Backpressure</td>
                    <td>Ingest buffer is full. SDK should retry via local disk/memory cache.</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}
