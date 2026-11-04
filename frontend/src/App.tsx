import { useState, useEffect } from 'react'
import { useAuth } from './auth/AuthContext'
import { Link } from './router'

const CURRENT_YEAR = new Date().getFullYear()

// SDK Integration Snippets
const SDK_DATA = {
  android: {
    name: 'Android (Kotlin)',
    desc: 'Lightweight Android SDK with automated Jetpack Compose UI state capture, OkHttp network interceptor, and ANR/crash telemetry.',
    code: `// build.gradle.kts
dependencies {
    implementation("io.artracker:artracker-android:0.2.0")
}

// Application.kt
class App : Application() {
    override fun onCreate() {
        super.onCreate()
        ARTracker.init(this) {
            endpoint = "https://artracker.exlm.my.id"
            appKey = "art_live_a892b109"
            maskAllInputFields = true
            captureNetworkPayloads = true
        }
    }
}`
  },
  ios: {
    name: 'iOS (Swift & SwiftUI)',
    desc: 'Swift package providing automated SwiftUI view tree snapshots, URLSession instrumentation, and memory pressure diagnostics.',
    code: `// Package.swift
dependencies: [
    .package(url: "https://github.com/tiofani03/artracker-ios.git", from: "0.2.0")
]

// AppDelegate.swift or App.swift
import ARTracker

@main
struct MyApp: App {
    init() {
        ARTracker.configure(
            endpoint: "https://artracker.exlm.my.id",
            appKey: "art_live_a892b109",
            maskSensitiveViews: true
        )
    }
    var body: some Scene { WindowGroup { ContentView() } }
}`
  },
  web: {
    name: 'Web (TypeScript / React)',
    desc: 'Zero-config Web SDK for React, Next.js, and modern SPAs. Captures rrweb DOM mutations, fetch/XHR, and unhandled errors.',
    code: `// npm install @artracker/browser
import { init as initARTracker } from '@artracker/browser'

initARTracker({
  endpoint: 'https://artracker.exlm.my.id',
  appKey: 'art_live_a892b109',
  sessionReplay: {
    sampleRate: 1.0,
    maskInputOptions: { password: true, creditCard: true }
  },
  networkTracking: { captureHeaders: false }
})`
  }
}

export default function App() {
  const { user } = useAuth()
  const [activeSdk, setActiveSdk] = useState<'android' | 'ios' | 'web'>('web')
  const [playbackTime, setPlaybackTime] = useState(14)
  const [isPlaying, setIsPlaying] = useState(true)
  const [activeInspectorTab, setActiveInspectorTab] = useState<'events' | 'network' | 'errors'>('events')
  
  // Interactive Simulator Log
  const [simLogs, setSimLogs] = useState<Array<{ id: number; ts: string; type: string; msg: string; tag: string }>>([
    { id: 1, ts: '10:14:02.102', type: 'info', msg: 'SDK initialized: session_token=sess_8f2910b', tag: 'INIT' },
    { id: 2, ts: '10:14:03.441', type: 'info', msg: 'User routed: /checkout/payment', tag: 'ROUTE' },
    { id: 3, ts: '10:14:05.120', type: 'warn', msg: 'Rage click detected on button#submit-order (4 clicks in 400ms)', tag: 'RAGE' },
  ])

  // Simulated player scrubber timer
  useEffect(() => {
    if (!isPlaying) return
    const interval = setInterval(() => {
      setPlaybackTime((prev) => (prev >= 60 ? 0 : prev + 1))
    }, 1000)
    return () => clearInterval(interval)
  }, [isPlaying])

  const triggerSimEvent = (type: string, tag: string, msg: string) => {
    const now = new Date()
    const ts = `${now.toTimeString().split(' ')[0]}.${String(now.getMilliseconds()).padStart(3, '0')}`
    setSimLogs((prev) => [
      { id: Date.now(), ts, type, msg, tag },
      ...prev.slice(0, 24)
    ])
  }

  return (
    <div>
      {/* Navbar */}
      <nav className="navbar">
        <div className="navbar-inner">
          <div className="brand">
            <div className="brand-icon">A</div>
            <span>ARTracker</span>
          </div>

          <div className="nav-links">
            <a href="#features" className="nav-link">Features</a>
            <a href="#player" className="nav-link">Session Replay</a>
            <a href="#sdks" className="nav-link">SDKs</a>
            <a href="#simulator" className="nav-link">Simulator</a>
            <a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer" className="nav-link">GitHub</a>
          </div>

          <div className="nav-actions">
            <Link to={user ? '/dashboard' : '/login'} className="btn btn-secondary btn-sm">
              {user ? 'Dashboard' : 'Login with SSO'}
            </Link>
            <a href="#simulator" className="btn btn-primary btn-sm">
              Live Console
            </a>
          </div>
        </div>
      </nav>

      {/* Hero */}
      <header className="hero">
        <div className="container">
          <div className="hero-badge">
            <span className="hero-badge-tag">ENGINE v0.2.0</span>
            <span>Self-hosted behavioral & dev telemetry</span>
          </div>

          <h1 className="hero-title">
            Open-Source Session Replay, Developer Diagnostics & Event Analytics
          </h1>

          <p className="hero-subtitle">
            A single, high-performance stack combining pixel-perfect DOM replay, Sentry-grade error diagnostics, and product analytics. No vendor lock-in, privacy-first by default.
          </p>

          <div className="hero-actions">
            <Link to={user ? '/dashboard' : '/login'} className="btn btn-primary">
              {user ? 'Open Dashboard' : 'Start with SSO'}
            </Link>
            <a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer" className="btn btn-secondary">
              Inspect on GitHub
            </a>
          </div>

          {/* Devtool Session Player Mockup */}
          <div id="player" className="preview-shell">
            <div className="preview-header">
              <div className="preview-dots">
                <div className="preview-dot" />
                <div className="preview-dot" />
                <div className="preview-dot" />
              </div>
              <div className="time-readout">
                session_id: rec_9082fd11 &bull; user: usr_lead_901 (Singapore)
              </div>
              <div className="preview-tabs">
                <button
                  className={`preview-tab-btn ${activeInspectorTab === 'events' ? 'active' : ''}`}
                  onClick={() => setActiveInspectorTab('events')}
                >
                  DOM Events
                </button>
                <button
                  className={`preview-tab-btn ${activeInspectorTab === 'network' ? 'active' : ''}`}
                  onClick={() => setActiveInspectorTab('network')}
                >
                  Network
                </button>
                <button
                  className={`preview-tab-btn ${activeInspectorTab === 'errors' ? 'active' : ''}`}
                  onClick={() => setActiveInspectorTab('errors')}
                >
                  Errors (1)
                </button>
              </div>
            </div>

            <div className="preview-body">
              {/* Left Screen Player */}
              <div className="player-pane">
                <div className="screen-viewport">
                  {/* Mock Replay Screen */}
                  <div className="simulated-card">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 14 }}>
                      <span style={{ fontSize: 13, fontWeight: 700 }}>Checkout Overview</span>
                      <span className="pill pill-orange">Step 3 of 3</span>
                    </div>

                    <div style={{ background: '#090B0F', border: '1px solid #1F2432', borderRadius: 4, padding: 12, marginBottom: 12 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-secondary)' }}>Enterprise License (Annual)</span>
                        <span style={{ fontFamily: 'var(--font-mono)' }}>$1,200.00</span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)' }}>
                        <span>VAT (0% Reverse Charge)</span>
                        <span>$0.00</span>
                      </div>
                    </div>

                    <div style={{ display: 'flex', gap: 8 }}>
                      <input
                        type="text"
                        readOnly
                        value="•••• •••• •••• 4242"
                        style={{ flex: 1, background: '#090B0F', border: '1px solid #1F2432', borderRadius: 4, padding: '8px 10px', fontSize: 11, color: '#9BA3AF', fontFamily: 'var(--font-mono)' }}
                      />
                      <button className="btn btn-primary btn-sm" style={{ pointerEvents: 'none' }}>
                        Pay Now
                      </button>
                    </div>

                    {/* Cursor element */}
                    <div
                      className="sim-cursor"
                      style={{
                        top: `${35 + (playbackTime % 20) * 2}%`,
                        left: `${50 + ((playbackTime * 4) % 35)}%`
                      }}
                    />
                  </div>
                </div>

                {/* Scrubber Controls */}
                <div className="player-controls">
                  <button
                    className="btn btn-secondary btn-sm"
                    style={{ padding: '4px 8px' }}
                    onClick={() => setIsPlaying(!isPlaying)}
                  >
                    {isPlaying ? 'Pause' : 'Play'}
                  </button>

                  <div className="scrubber-track" onClick={(e) => {
                    const rect = e.currentTarget.getBoundingClientRect()
                    const pos = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
                    setPlaybackTime(Math.round(pos * 60))
                  }}>
                    <div className="scrubber-fill" style={{ width: `${(playbackTime / 60) * 100}%` }} />
                    <div className="scrubber-marker marker-rage" style={{ left: '25%' }} title="Rage Click" />
                    <div className="scrubber-marker marker-error" style={{ left: '78%' }} title="500 Internal Error" />
                  </div>

                  <span className="time-readout">
                    00:{String(playbackTime).padStart(2, '0')} / 01:00
                  </span>
                </div>
              </div>

              {/* Right Telemetry Pane */}
              <div className="inspector-pane">
                <div className="inspector-header">
                  <span>Inspector Stream</span>
                  <span>Filtered ({activeInspectorTab})</span>
                </div>

                <div className="inspector-list">
                  {activeInspectorTab === 'events' && (
                    <>
                      <div className="inspector-row">
                        <span className="pill pill-orange">CLICK</span>
                        <span style={{ color: 'var(--text-secondary)' }}>button#submit-order</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:15</span>
                      </div>
                      <div className="inspector-row">
                        <span className="pill pill-warn">RAGE</span>
                        <span style={{ color: 'var(--text-secondary)' }}>4 clicks within 400ms</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:15</span>
                      </div>
                      <div className="inspector-row">
                        <span className="pill pill-info">NAV</span>
                        <span style={{ color: 'var(--text-secondary)' }}>popstate /billing</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:10</span>
                      </div>
                      <div className="inspector-row">
                        <span className="pill pill-success">MUT</span>
                        <span style={{ color: 'var(--text-secondary)' }}>DOM Node Insert: modal-backdrop</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:08</span>
                      </div>
                    </>
                  )}

                  {activeInspectorTab === 'network' && (
                    <>
                      <div className="inspector-row">
                        <span className="pill pill-error">500</span>
                        <span style={{ color: 'var(--text-secondary)' }}>POST /api/v1/charge (218ms)</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:46</span>
                      </div>
                      <div className="inspector-row">
                        <span className="pill pill-success">200</span>
                        <span style={{ color: 'var(--text-secondary)' }}>GET /api/v1/cart (42ms)</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:12</span>
                      </div>
                      <div className="inspector-row">
                        <span className="pill pill-success">200</span>
                        <span style={{ color: 'var(--text-secondary)' }}>POST /api/v1/telemetry (18ms)</span>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:04</span>
                      </div>
                    </>
                  )}

                  {activeInspectorTab === 'errors' && (
                    <>
                      <div className="inspector-row" style={{ background: 'rgba(239, 68, 68, 0.05)' }}>
                        <span className="pill pill-error">CRASH</span>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                          <span style={{ color: 'var(--red)', fontWeight: 600 }}>PaymentGatewayTimeout: Stripe upstream unreachable</span>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>at PaymentForm.tsx:142:18</span>
                        </div>
                        <span style={{ marginLeft: 'auto', color: 'var(--text-muted)' }}>00:46</span>
                      </div>
                    </>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      </header>

      {/* 6 Core Pillars */}
      <section id="features" className="section">
        <div className="container">
          <div className="section-label">Architecture &amp; Features</div>
          <h2 className="section-title">Six Pillars of Observability in One Engine</h2>
          <p className="section-desc">
            Designed for engineering and growth teams. No siloed SaaS tools — correlate user sessions directly with crash dumps and marketing funnels.
          </p>

          <div className="pillars-grid">
            {/* 1. SSO */}
            <div className="pillar-card">
              <div className="pillar-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
              </div>
              <h3 className="pillar-title">1. Enterprise SSO</h3>
              <p className="pillar-text">
                Centralized team authentication with Google SSO. Enforce granular Role-Based Access Control (RBAC).
              </p>
              <div className="pillar-meta">Auth / Identity / RBAC</div>
            </div>

            {/* 2. Session Replay */}
            <div className="pillar-card">
              <div className="pillar-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
              </div>
              <h3 className="pillar-title">2. Session Replay</h3>
              <p className="pillar-text">
                OpenReplay-grade visual reproduction. Reconstruct exact DOM mutations, rage clicks, mouse speed, mobile touch interactions, and client latency.
              </p>
              <div className="pillar-meta">DOM Mutations / Rage Clicks</div>
            </div>

            {/* 3. Dev Event Log */}
            <div className="pillar-card">
              <div className="pillar-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>
              </div>
              <h3 className="pillar-title">3. Developer Event Log</h3>
              <p className="pillar-text">
                Combines OpenReplay telemetry with Sentry-style error capture. Demangle stack traces via Source Maps, inspect network requests, and trace exceptions.
              </p>
              <div className="pillar-meta">Sentry-Grade / Source Maps</div>
            </div>

            {/* 4. Event Tracker */}
            <div className="pillar-card">
              <div className="pillar-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>
              </div>
              <h3 className="pillar-title">4. Marketing Event Tracker</h3>
              <p className="pillar-text">
                Mixpanel &amp; MoEngage-style event ingestion. Map user conversion funnels, track cohort retention, inspect UTM parameters, and calculate feature drop-off.
              </p>
              <div className="pillar-meta">Funnels / Cohorts / Retention</div>
            </div>

            {/* 5. Custom Dashboards */}
            <div className="pillar-card">
              <div className="pillar-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="9"/><rect x="14" y="3" width="7" height="5"/><rect x="14" y="12" width="7" height="9"/><rect x="3" y="16" width="7" height="5"/></svg>
              </div>
              <h3 className="pillar-title">5. Custom Dashboards</h3>
              <p className="pillar-text">
                Build flexible monitoring dashboards. Track crash-free session rates, p50/p95/p99 network percentiles, and regional distribution with live metrics.
              </p>
              <div className="pillar-meta">Live Metrics / Custom Widgets</div>
            </div>

            {/* 6. Custom Alerting */}
            <div className="pillar-card">
              <div className="pillar-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
              </div>
              <h3 className="pillar-title">6. Intelligent Alerts</h3>
              <p className="pillar-text">
                Direct dispatch to Telegram bots, Slack webhooks, and PagerDuty when unhandled crash spikes occur or when conversion rates drop below threshold.
              </p>
              <div className="pillar-meta">Telegram / Slack / Webhook</div>
            </div>
          </div>
        </div>
      </section>

      {/* Multi-Platform SDKs */}
      <section id="sdks" className="section">
        <div className="container">
          <div className="section-label">SDK Ecosystem</div>
          <h2 className="section-title">Drop-In SDKs for Native &amp; Web</h2>
          <p className="section-desc">
            Instrument your applications with minimal lines of code. Privacy filters automatically mask sensitive fields before data leaves the device.
          </p>

          <div className="sdk-box">
            <div className="sdk-nav">
              <button
                className={`sdk-tab ${activeSdk === 'android' ? 'active' : ''}`}
                onClick={() => setActiveSdk('android')}
              >
                <span>Android (Kotlin)</span>
              </button>
              <button
                className={`sdk-tab ${activeSdk === 'ios' ? 'active' : ''}`}
                onClick={() => setActiveSdk('ios')}
              >
                <span>iOS (Swift)</span>
              </button>
              <button
                className={`sdk-tab ${activeSdk === 'web' ? 'active' : ''}`}
                onClick={() => setActiveSdk('web')}
              >
                <span>Web (TypeScript)</span>
              </button>
            </div>

            <div className="sdk-content">
              <div>
                <h3 style={{ fontSize: 18, fontWeight: 700, marginBottom: 8 }}>{SDK_DATA[activeSdk].name}</h3>
                <p style={{ fontSize: 14, color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: 20 }}>
                  {SDK_DATA[activeSdk].desc}
                </p>

                <ul style={{ listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 10, fontSize: 13, color: 'var(--text-secondary)' }}>
                  <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: 'var(--orange)', fontWeight: 700 }}>&bull;</span>
                    Automatic PII masking on password &amp; payment inputs
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: 'var(--orange)', fontWeight: 700 }}>&bull;</span>
                    Adaptive batching with zero UI-thread stutter
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: 'var(--orange)', fontWeight: 700 }}>&bull;</span>
                    Offline buffering with automatic retry backoff
                  </li>
                </ul>
              </div>

              <div className="code-block">
                <pre>{SDK_DATA[activeSdk].code}</pre>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Simulator Section */}
      <section id="simulator" className="section">
        <div className="container">
          <div className="section-label">Interactive Sandbox</div>
          <h2 className="section-title">Test Event Ingestion in Real-Time</h2>
          <p className="section-desc">
            Simulate client-side interactions below. See how events, exceptions, and marketing funnels stream directly into the ARTracker pipeline.
          </p>

          <div className="sim-grid">
            <div className="sim-actions">
              <button
                className="sim-btn"
                onClick={() => triggerSimEvent('warn', 'RAGE', 'User triggered 5 rapid clicks on button.cta-checkout')}
              >
                <span>Trigger Rage Click</span>
                <span className="pill pill-warn">WARN</span>
              </button>

              <button
                className="sim-btn"
                onClick={() => triggerSimEvent('error', '500 FATAL', 'Unhandled rejection: PGConnectionPoolExhausted at /api/v1/ingest')}
              >
                <span>Simulate 500 Backend Crash</span>
                <span className="pill pill-error">ERROR</span>
              </button>

              <button
                className="sim-btn"
                onClick={() => triggerSimEvent('success', 'TRACK', 'Event fired: checkout_completed (revenue: $120.00, plan: pro)')}
              >
                <span>Fire Marketing Conversion</span>
                <span className="pill pill-success">EVENT</span>
              </button>

              <button
                className="sim-btn"
                onClick={() => triggerSimEvent('info', 'ALERT', 'Trigger dispatched to Telegram group -5381106692')}
              >
                <span>Dispatch Custom Alert</span>
                <span className="pill pill-orange">ALERT</span>
              </button>
            </div>

            <div className="sim-terminal">
              <div style={{ color: 'var(--text-muted)', marginBottom: 12, borderBottom: '1px solid #1C202C', paddingBottom: 6 }}>
                // Live Telemetry Event Stream &bull; Ready
              </div>
              {simLogs.map((log) => (
                <div key={log.id} className="sim-line">
                  <span className="sim-ts">[{log.ts}]</span>
                  <span className={`pill ${
                    log.type === 'error' ? 'pill-error' :
                    log.type === 'warn' ? 'pill-warn' :
                    log.type === 'success' ? 'pill-success' : 'pill-orange'
                  }`}>
                    {log.tag}
                  </span>
                  <span style={{ color: log.type === 'error' ? 'var(--red)' : '#D1D5DB' }}>
                    {log.msg}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* Footer */}
      <footer className="footer">
        <div className="container">
          <div className="footer-inner">
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <div className="brand-icon" style={{ width: 20, height: 20, fontSize: 11 }}>A</div>
              <span style={{ fontWeight: 700 }}>ARTracker</span>
              <span style={{ color: 'var(--text-muted)' }}>&bull; Open-source telemetry platform</span>
            </div>

            <div style={{ display: 'flex', gap: 20 }}>
              <a href="#features" className="nav-link">Features</a>
              <a href="#sdks" className="nav-link">SDKs</a>
              <a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer" className="nav-link">GitHub</a>
            </div>

            <div>
              &copy; {CURRENT_YEAR} ARTracker. MIT Licensed.
            </div>
          </div>
        </div>
      </footer>
    </div>
  )
}
