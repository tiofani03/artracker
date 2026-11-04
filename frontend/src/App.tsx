import { useState, useEffect } from 'react'

const CURRENT_YEAR = new Date().getFullYear()

// Available SDK Code Snippets
const SDK_SNIPPETS = {
  android: {
    title: 'Android Native (Kotlin & Jetpack Compose)',
    lang: 'kotlin',
    file: 'build.gradle.kts & MainActivity.kt',
    code: `// 1. Tambahkan dependency
dependencies {
    implementation("io.artracker:artracker-android:1.2.0")
}

// 2. Inisialisasi di Application atau MainActivity
class MainApp : Application() {
    override fun onCreate() {
        super.onCreate()
        ARTracker.init(this) {
            projectId = "art_live_89f02a"
            endpoint = "https://artracker.exlm.my.id"
            enableSessionReplay = true
            maskSensitiveInputs = true // Sensor password & credit card otomatis
            trackNetworkCalls = true
        }
    }
}`,
    highlights: [
      'Native gesture & touch-heatmap recording',
      'ANR & Uncaught Exception stacktrace capture',
      'Automatic compose recomposition tracking',
      'Ultra-low memory footprint (< 12MB RAM)',
    ],
  },
  ios: {
    title: 'iOS Native (Swift & SwiftUI)',
    lang: 'swift',
    file: 'Package.swift & App.swift',
    code: `// 1. Import ARTracker via SPM
import ARTrackerSDK

// 2. Inisialisasi saat app lifecycle boot
@main
struct MyApp: App {
    init() {
        ARTracker.start(
            projectId: "art_live_89f02a",
            endpoint: "https://artracker.exlm.my.id",
            options: [
                .sessionReplay: true,
                .maskAllTextInputs: true,
                .crashReporting: true
            ]
        )
    }

    var body: some Scene {
        WindowGroup { ContentView() }
    }
}`,
    highlights: [
      'SwiftUI & UIKit view hierarchy serialization',
      'Crashlytics-grade Mach exception & signal handling',
      'Client-side PII redactor (Zero sensitive data leak)',
      'Metal-accelerated replay frame compressor',
    ],
  },
  web: {
    title: 'Web SDK (React, Vue, Next.js, Vanilla TS)',
    lang: 'typescript',
    file: 'src/main.ts',
    code: `import { initARTracker } from '@artracker/web'

// Inisialisasi di entrypoint frontend
const tracker = initARTracker({
  projectId: 'art_live_89f02a',
  endpoint: 'https://artracker.exlm.my.id',
  sessionReplay: {
    sampleRate: 1.0, // Rekam 100% session
    maskAllInputs: true,
    recordCanvas: false,
    networkPayloads: true
  },
  devErrors: {
    captureUnhandled: true,
    sourceMapsEnabled: true
  }
})

// Track custom business / marketing event
tracker.event('campaign_checkout_initiated', {
  plan: 'Enterprise Pro',
  amount: 499,
  currency: 'USD',
  campaign_source: 'google_cpc'
})`,
    highlights: [
      'Zero-overhead rrweb DOM snapshot engine',
      'Full Sentry-style breadcrumb & network waterfall',
      'Mixpanel-compatible funnel & user attribute tracker',
      'Single lightweight bundle (< 18KB gzip)',
    ],
  },
}

interface SimulatedLog {
  id: string
  time: string
  level: 'info' | 'warn' | 'error' | 'success'
  message: string
}

export default function App() {
  const [activeSdk, setActiveSdk] = useState<'android' | 'ios' | 'web'>('web')
  const [activeTab, setActiveTab] = useState<'events' | 'network' | 'console'>('events')
  const [timelinePos, setTimelinePos] = useState(62)
  const [isSsoModalOpen, setIsSsoModalOpen] = useState(false)
  
  // Interactive Simulator Logs
  const [logs, setLogs] = useState<SimulatedLog[]>([
    { id: '1', time: '10:04:12', level: 'info', message: 'SDK initialized: session_id=ses_99af28b0' },
    { id: '2', time: '10:04:13', level: 'info', message: 'DOM snapshot captured (142 nodes, 18kb gzip)' },
    { id: '3', time: '10:04:15', level: 'warn', message: 'High latency detected: POST /api/v1/auth/sso took 740ms' },
    { id: '4', time: '10:04:18', level: 'success', message: 'Event tracked: [marketing] campaign_clicked {utm_source: "x_ads"}' },
  ])

  // Animated cursor mock position
  const [cursorPos, setCursorPos] = useState({ x: 180, y: 130 })

  useEffect(() => {
    const timer = setInterval(() => {
      setCursorPos({
        x: Math.floor(80 + Math.random() * 220),
        y: Math.floor(60 + Math.random() * 140),
      })
    }, 2800)
    return () => clearInterval(timer)
  }, [])

  function addLog(level: SimulatedLog['level'], message: string) {
    const now = new Date().toTimeString().split(' ')[0]
    setLogs((prev) => [
      { id: Math.random().toString(), time: now, level, message },
      ...prev.slice(0, 7),
    ])
  }

  function simulateRageClick() {
    addLog('warn', '🚨 RAGE CLICK DETECTED: 5 consecutive clicks in 400ms on button#pay-now')
    setTimelinePos(85)
  }

  function simulateError() {
    addLog('error', '💥 FATAL: Uncaught TypeError: Cannot read properties of undefined (reading "user_token")')
    setTimelinePos(35)
  }

  function simulateMarketingEvent() {
    addLog('success', '🎯 FUNNEL STEP 3: "plan_upgraded" { tier: "Enterprise", mrr: 1200 }')
  }

  function simulateAlert() {
    addLog('info', '🔔 ALERT TRIGGERED: Sent to Telegram channel & Slack #dev-alerts (Error rate > 2%)')
  }

  return (
    <>
      <div className="ambient-glow" />
      <div className="grid-overlay" />

      {/* Navigation */}
      <nav className="navbar">
        <div className="container nav-wrapper">
          <a href="#" className="brand">
            <div className="brand-icon-wrapper">
              <svg width="22" height="22" viewBox="0 0 64 64" fill="none">
                <circle cx="32" cy="32" r="24" stroke="#FF5500" strokeWidth="4" strokeDasharray="6 6" opacity="0.6" />
                <path d="M22 44 L32 18 L42 44 M26 35 L38 35" stroke="#FF5500" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
                <circle cx="32" cy="32" r="4" fill="#FFFFFF" />
              </svg>
            </div>
            <span className="brand-name">AR<span>Tracker</span></span>
          </a>

          <ul className="nav-links">
            <li><a href="#features">Features</a></li>
            <li><a href="#replay">Session Replay</a></li>
            <li><a href="#sdk">SDKs</a></li>
            <li><a href="#simulator">Live Simulator</a></li>
            <li><a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer">GitHub</a></li>
          </ul>

          <div className="nav-actions">
            <button className="btn btn-ghost" onClick={() => setIsSsoModalOpen(true)}>
              Sign In (SSO)
            </button>
            <button className="btn btn-primary" onClick={() => setIsSsoModalOpen(true)}>
              Get Started Free
            </button>
          </div>
        </div>
      </nav>

      {/* Hero Section */}
      <header className="hero">
        <div className="container">
          <div className="hero-content">
            <div className="badge badge-orange">
              <span className="pulse-dot" />
              <span>Next-Gen Observability & Replay Engine</span>
            </div>

            <h1 className="hero-title">
              See Every Click. Diagnose Every Crash.<br />
              <span className="gradient-text">Understand Every User.</span>
            </h1>

            <p className="hero-subtitle">
              Satu engine terpadu untuk tim engineering dan marketing: gabungkan <strong>Session Replay</strong> berkualitas pixel-perfect, <strong>Developer Event & Crash Log</strong> setara Sentry, dan <strong>Marketing Event Tracker</strong> seperti Mixpanel/MoEngage.
            </p>

            <div className="hero-cta">
              <button className="btn btn-primary btn-lg" onClick={() => setIsSsoModalOpen(true)}>
                <span>Start Free with SSO</span>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <line x1="5" y1="12" x2="19" y2="12"></line>
                  <polyline points="12 5 19 12 12 19"></polyline>
                </svg>
              </button>

              <a href="#simulator" className="btn btn-secondary btn-lg">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polygon points="5 3 19 12 5 21 5 3"></polygon>
                </svg>
                <span>Try Live Simulator</span>
              </a>

              <a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer" className="btn btn-ghost btn-lg">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
                </svg>
                <span>Open Source</span>
              </a>
            </div>
          </div>

          {/* Interactive Replay Player Showcase */}
          <div className="player-preview-wrapper" id="replay">
            <div className="player-preview-card">
              {/* Header */}
              <div className="player-header">
                <div className="player-dots">
                  <div className="dot dot-red" />
                  <div className="dot dot-yellow" />
                  <div className="dot dot-green" />
                </div>
                <div className="player-meta">
                  <span>Session: <strong>ses_88a4e91c</strong></span>
                  <span>User: <strong>achdi@team.id (macOS / Chrome 124)</strong></span>
                  <span className="live-pill">
                    <span className="pulse-dot" style={{ width: 6, height: 6 }} />
                    REPLAY ENGINE ACTIVE
                  </span>
                </div>
                <div style={{ fontSize: '0.82rem', color: '#8E9BAE' }}>
                  Viewport: <strong>1440 × 900 (100%)</strong>
                </div>
              </div>

              {/* Body */}
              <div className="player-body">
                {/* Replay Viewport */}
                <div className="replay-viewport">
                  <div className="mock-app-screen">
                    {/* Simulated Cursor */}
                    <div 
                      className="mock-cursor"
                      style={{ transform: `translate(${cursorPos.x}px, ${cursorPos.y}px)` }}
                    >
                      <svg width="20" height="20" viewBox="0 0 24 24" fill="#FF5500" stroke="#FFFFFF" strokeWidth="1.5">
                        <path d="M3 3l7 18 3-7 7-3L3 3z" />
                      </svg>
                      <div className="cursor-tag">Achdi (Jakarta, ID)</div>
                    </div>

                    <div className="mock-checkout-box">
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
                        <span style={{ fontSize: '0.85rem', color: '#9DA8BA' }}>Checkout Flow #4012</span>
                        <span style={{ fontSize: '0.85rem', color: '#FF782E', fontWeight: 600 }}>Step 2 of 3</span>
                      </div>
                      <div style={{ height: 18, background: 'rgba(255,255,255,0.06)', borderRadius: 4, marginBottom: 10 }} />
                      <div style={{ height: 36, background: 'rgba(255,255,255,0.04)', borderRadius: 6, marginBottom: 14, border: '1px solid rgba(255,255,255,0.08)' }} />
                      <div style={{ display: 'flex', gap: 10 }}>
                        <div style={{ flex: 1, height: 38, background: 'var(--primary-gradient)', borderRadius: 6, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#FFF', fontSize: '0.82rem', fontWeight: 600 }}>
                          Complete Purchase
                        </div>
                        <div style={{ width: 70, height: 38, background: 'rgba(255,255,255,0.05)', borderRadius: 6 }} />
                      </div>
                    </div>
                  </div>

                  {/* Scrubber Bar */}
                  <div className="player-timeline-bar">
                    <button style={{ background: 'none', border: 'none', color: '#FF5500', cursor: 'pointer', display: 'flex', alignItems: 'center' }}>
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
                        <polygon points="5 3 19 12 5 21 5 3"></polygon>
                      </svg>
                    </button>
                    <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: '#8E9BAE' }}>
                      01:24 / 02:45
                    </span>
                    <div 
                      className="timeline-track"
                      onClick={(e) => {
                        const rect = e.currentTarget.getBoundingClientRect()
                        const pct = ((e.clientX - rect.left) / rect.width) * 100
                        setTimelinePos(Math.round(pct))
                      }}
                    >
                      <div className="timeline-progress" style={{ width: `${timelinePos}%` }} />
                      <div className="timeline-marker marker-nav" title="Navigation Event" />
                      <div className="timeline-marker marker-error" title="Error Logged" />
                      <div className="timeline-marker marker-click" title="Rage Click" />
                    </div>
                    <span style={{ fontSize: '0.8rem', color: '#8E9BAE' }}>1.0x</span>
                  </div>
                </div>

                {/* Event Sidebar Tabs */}
                <div className="events-panel">
                  <div className="panel-tabs">
                    <button 
                      className={`panel-tab ${activeTab === 'events' ? 'active' : ''}`}
                      onClick={() => setActiveTab('events')}
                    >
                      DOM Events (12)
                    </button>
                    <button 
                      className={`panel-tab ${activeTab === 'network' ? 'active' : ''}`}
                      onClick={() => setActiveTab('network')}
                    >
                      Network (8)
                    </button>
                    <button 
                      className={`panel-tab ${activeTab === 'console' ? 'active' : ''}`}
                      onClick={() => setActiveTab('console')}
                    >
                      Console (2)
                    </button>
                  </div>

                  <div className="events-list">
                    {activeTab === 'events' && (
                      <>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-track">MARKETING</span>
                            <span className="event-time">01:22.4</span>
                          </div>
                          <div>track("checkout_flow_viewed")</div>
                        </div>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-click">USER CLICK</span>
                            <span className="event-time">01:20.1</span>
                          </div>
                          <div>click button#pay-now (x: 480, y: 310)</div>
                        </div>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-error">RAGE CLICK</span>
                            <span className="event-time">01:18.9</span>
                          </div>
                          <div>5 fast clicks detected on #btn-submit</div>
                        </div>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-click">INPUT</span>
                            <span className="event-time">01:14.2</span>
                          </div>
                          <div>change input[name="promo_code"] → "SUMMER50"</div>
                        </div>
                      </>
                    )}

                    {activeTab === 'network' && (
                      <>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-network">200 OK</span>
                            <span className="event-time">142ms</span>
                          </div>
                          <div>POST /api/v1/checkout/validate</div>
                        </div>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-error">500 ERR</span>
                            <span className="event-time">820ms</span>
                          </div>
                          <div>POST /api/v1/payment/charge</div>
                        </div>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-network">204 OK</span>
                            <span className="event-time">45ms</span>
                          </div>
                          <div>POST /v1/telemetry/ingest</div>
                        </div>
                      </>
                    )}

                    {activeTab === 'console' && (
                      <>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-error">EXCEPTION</span>
                            <span className="event-time">01:20.2</span>
                          </div>
                          <div style={{ color: '#F43F5E' }}>PaymentGateError: Gateway timeout after 800ms</div>
                        </div>
                        <div className="event-item">
                          <div className="event-item-header">
                            <span className="event-type type-track">LOG</span>
                            <span className="event-time">01:12.0</span>
                          </div>
                          <div>[Auth] Token refreshed via SSO session</div>
                        </div>
                      </>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </header>

      {/* 6 Core Pillars Section */}
      <section className="section" id="features">
        <div className="container">
          <div className="section-header">
            <div className="badge badge-orange">
              <span>6 Essential Capabilities</span>
            </div>
            <h2 className="section-title">Built for Engineers. Cherished by Marketers.</h2>
            <p className="section-subtitle">
              Tidak perlu memisahkan tool developer diagnostic dengan marketing campaign tracker. ARTracker menyatukan semuanya dalam satu arsitektur terpusat.
            </p>
          </div>

          <div className="features-grid">
            {/* 1. Login with SSO */}
            <div className="feature-card">
              <div className="feature-icon-box">
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
                </svg>
              </div>
              <span className="feature-tag">Security & Identity</span>
              <h3>1. Login with SSO</h3>
              <p>Autentikasi enterprise tanpa hambatan. Terintegrasi penuh dengan Google Workspace, GitHub OAuth, SAML 2.0, Okta, dan generic OIDC.</p>
              <ul className="feature-bullets">
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Role-Based Access Control (RBAC)</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Multi-tenant workspace isolation</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>MFA & session token revocation</span>
                </li>
              </ul>
            </div>

            {/* 2. Session Replay */}
            <div className="feature-card">
              <div className="feature-icon-box">
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="10"/>
                  <polygon points="10 8 16 12 10 16 10 8"/>
                </svg>
              </div>
              <span className="feature-tag">OpenReplay-Grade</span>
              <h3>2. Session Replay</h3>
              <p>Putar ulang rekaman aksi user secara visual. Rekam setiap klik, scroll, dead clicks, rage clicks, dan perubahan DOM state tanpa mengorbankan privasi.</p>
              <ul className="feature-bullets">
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Client-side PII & password masking</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Rage & dead click detection</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Mobile touch heatmap & gestures</span>
                </li>
              </ul>
            </div>

            {/* 3. Event Log (Dev Diagnostics) */}
            <div className="feature-card">
              <div className="feature-icon-box">
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                  <polyline points="14 2 14 8 20 8"/>
                  <line x1="16" y1="13" x2="8" y2="13"/>
                  <line x1="16" y1="17" x2="8" y2="17"/>
                  <polyline points="10 9 9 9 8 9"/>
                </svg>
              </div>
              <span className="feature-tag">Sentry + OpenReplay</span>
              <h3>3. Developer Event Log</h3>
              <p>Gabungan log komprehensif untuk developer. Tangkap uncaught runtime exceptions, stack trace dengan source maps, dan network request failure payloads.</p>
              <ul className="feature-bullets">
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Automatic Source-Map deobfuscation</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Network payload & headers inspection</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Breadcrumb timeline preceding crashes</span>
                </li>
              </ul>
            </div>

            {/* 4. Event Tracker (Marketing) */}
            <div className="feature-card">
              <div className="feature-icon-box">
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21.21 15.89A10 10 0 1 1 8 2.83"/>
                  <path d="M22 12A10 10 0 0 0 12 2v10z"/>
                </svg>
              </div>
              <span className="feature-tag">Mixpanel & MoEngage</span>
              <h3>4. Marketing Event Tracker</h3>
              <p>Lacak interaksi user untuk conversion tracking dan kampanye pemasaran. Bangun funnel analisis, retensi user kohort, dan atribusi kampanye.</p>
              <ul className="feature-bullets">
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Funnel conversion & drop-off analysis</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>UTM & ad campaign attribution</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>User persona & cohort segmentation</span>
                </li>
              </ul>
            </div>

            {/* 5. Custom Dashboard */}
            <div className="feature-card">
              <div className="feature-icon-box">
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="3" y="3" width="7" height="9"/>
                  <rect x="14" y="3" width="7" height="5"/>
                  <rect x="14" y="12" width="7" height="9"/>
                  <rect x="3" y="16" width="7" height="5"/>
                </svg>
              </div>
              <span className="feature-tag">Real-Time Metrics</span>
              <h3>5. Custom Dashboard</h3>
              <p>Susun visualisasi dashboard sesuai kebutuhan tim dev maupun bisnis. Pantau metrik crash-free rate, latency p95/p99, dan active users secara live.</p>
              <ul className="feature-bullets">
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Drag-and-drop widget customizer</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Sub-second real-time metric streams</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Shareable team & executive boards</span>
                </li>
              </ul>
            </div>

            {/* 6. Custom Alert */}
            <div className="feature-card">
              <div className="feature-icon-box">
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/>
                  <path d="M13.73 21a2 2 0 0 1-3.46 0"/>
                </svg>
              </div>
              <span className="feature-tag">Instant Dispatch</span>
              <h3>6. Custom Alert</h3>
              <p>Dapatkan sinyal peringatan seketika saat terjadi lonjakan error atau penurunan drastis pada funnel checkout. Dukungan penuh webhook dan bot.</p>
              <ul className="feature-bullets">
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Direct Telegram & Slack notifications</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Threshold anomaly & spike algorithms</span>
                </li>
                <li>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12"/></svg>
                  <span>Custom HTTP Webhook triggers</span>
                </li>
              </ul>
            </div>
          </div>
        </div>
      </section>

      {/* Multi-Platform SDK Section */}
      <section className="section" id="sdk">
        <div className="container">
          <div className="section-header">
            <div className="badge badge-orange">
              <span>Universal Client SDKs</span>
            </div>
            <h2 className="section-title">Drop-In SDKs for Native & Web</h2>
            <p className="section-subtitle">
              Satu standard payload untuk semua platform. Integrasi dalam hitungan menit di Android, iOS, dan Web app kamu.
            </p>
          </div>

          <div className="sdk-box">
            <div className="sdk-nav">
              <button 
                className={`sdk-tab-btn ${activeSdk === 'android' ? 'active' : ''}`}
                onClick={() => setActiveSdk('android')}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M17.523 15.3414c-.5511 0-.9993-.4486-.9993-.9997s.4482-.9993.9993-.9993c.551 0 .9993.4482.9993.9993.0001.5511-.4483.9997-.9993.9997m-11.046 0c-.5511 0-.9993-.4486-.9993-.9997s.4482-.9993.9993-.9993c.5511 0 .9993.4482.9993.9993 0 .5511-.4482.9997-.9993.9997m11.4045-6.02l1.9973-3.4592a.416.416 0 00-.1521-.5676.416.416 0 00-.5676.1521l-2.0223 3.503C15.5902 8.4116 13.8533 8.1 12 8.1s-3.5902.3116-5.1367.8497L4.841 5.4467a.4161.4161 0 00-.5677-.1521.4157.4157 0 00-.1521.5676l1.9973 3.4592C2.6889 11.1867.3432 14.6589 0 18.761h24c-.3432-4.1021-2.6889-7.5743-6.1185-9.4396"/>
                </svg>
                <span>Android (Kotlin)</span>
              </button>

              <button 
                className={`sdk-tab-btn ${activeSdk === 'ios' ? 'active' : ''}`}
                onClick={() => setActiveSdk('ios')}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.81-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M15.97 6.37c.62-.75 1.04-1.8 0.92-2.85-.9.04-1.98.6-2.62 1.35-.57.65-1.06 1.72-.93 2.74 1 .08 2.01-.49 2.63-1.24"/>
                </svg>
                <span>iOS (Swift)</span>
              </button>

              <button 
                className={`sdk-tab-btn ${activeSdk === 'web' ? 'active' : ''}`}
                onClick={() => setActiveSdk('web')}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <circle cx="12" cy="12" r="10"/>
                  <line x1="2" y1="12" x2="22" y2="12"/>
                  <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
                </svg>
                <span>Web (TypeScript)</span>
              </button>
            </div>

            <div className="sdk-body">
              <div className="sdk-details">
                <h4>{SDK_SNIPPETS[activeSdk].title}</h4>
                <p>
                  SDK didesain ringan, zero-dependency, dan tidak pernah membebani performa frame rate aplikasi ataupun user experience.
                </p>
                <ul className="sdk-features-list">
                  {SDK_SNIPPETS[activeSdk].highlights.map((item, idx) => (
                    <li key={idx}>
                      <span style={{ color: 'var(--primary)', fontWeight: 'bold' }}>✓</span>
                      <span>{item}</span>
                    </li>
                  ))}
                </ul>
              </div>

              <div className="sdk-code-wrapper">
                <div className="code-header">
                  <span>{SDK_SNIPPETS[activeSdk].file}</span>
                  <span style={{ color: '#FF782E' }}>3 lines integration</span>
                </div>
                <pre className="code-content">
                  <code>{SDK_SNIPPETS[activeSdk].code}</code>
                </pre>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Interactive Simulator Section */}
      <section className="section simulator-section" id="simulator">
        <div className="container">
          <div className="section-header">
            <div className="badge badge-orange">
              <span>Interactive Playground</span>
            </div>
            <h2 className="section-title">Test Drive Ingestion Engine</h2>
            <p className="section-subtitle">
              Klik trigger tombol di bawah untuk melihat bagaimana ARTracker menangkap anomali, klik frustrasi (rage click), dan event marketing secara real-time.
            </p>
          </div>

          <div className="simulator-box">
            <div className="sim-controls">
              <h4>Simulate Real Events</h4>
              <p>Pilih aksi untuk mengirim paket payload telemetri dummy ke buffer pipeline:</p>

              <div className="sim-btn-grid">
                <button className="sim-action-btn" onClick={simulateRageClick}>
                  <span>⚡ Fire Rage Click</span>
                  <span style={{ fontSize: '0.75rem', color: 'var(--primary)' }}>+5 clicks/s</span>
                </button>

                <button className="sim-action-btn" onClick={simulateError}>
                  <span>💥 Simulate 500 Fatal Crash</span>
                  <span style={{ fontSize: '0.75rem', color: '#F43F5E' }}>Sentry stack</span>
                </button>

                <button className="sim-action-btn" onClick={simulateMarketingEvent}>
                  <span>🎯 Track Funnel Conversion</span>
                  <span style={{ fontSize: '0.75rem', color: '#10B981' }}>Marketing event</span>
                </button>

                <button className="sim-action-btn" onClick={simulateAlert}>
                  <span>🔔 Trigger Threshold Alert</span>
                  <span style={{ fontSize: '0.75rem', color: '#38BDF8' }}>Telegram/Slack</span>
                </button>
              </div>
            </div>

            <div className="sim-terminal">
              <div className="terminal-header">
                <div style={{ display: 'flex', gap: 6 }}>
                  <div className="dot dot-red" style={{ width: 8, height: 8 }} />
                  <div className="dot dot-yellow" style={{ width: 8, height: 8 }} />
                  <div className="dot dot-green" style={{ width: 8, height: 8 }} />
                </div>
                <span>LIVE TELEMETRY STREAM</span>
                <span style={{ color: '#10B981' }}>● Ingesting</span>
              </div>

              <div className="terminal-body">
                {logs.map((log) => (
                  <div key={log.id} className="term-line">
                    <span className="term-time">[{log.time}]</span>
                    <span className={`term-level-${log.level}`}>[{log.level.toUpperCase()}]</span>
                    <span className="term-msg">{log.message}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* CTA Banner */}
      <div className="container">
        <div className="cta-banner">
          <h2>Ready to Observe Without Limits?</h2>
          <p>
            Deploy ARTracker di VPS kamu sendiri atau pakai cloud cluster kami. Full data ownership, zero vendor lock-in.
          </p>
          <div style={{ display: 'flex', gap: 16, justifyContent: 'center', flexWrap: 'wrap' }}>
            <button className="btn btn-primary btn-lg" onClick={() => setIsSsoModalOpen(true)}>
              Login with SSO
            </button>
            <a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer" className="btn btn-secondary btn-lg">
              Explore Repository
            </a>
          </div>
        </div>
      </div>

      {/* Footer */}
      <footer className="footer">
        <div className="container">
          <div className="footer-top">
            <div className="brand">
              <div className="brand-icon-wrapper" style={{ width: 32, height: 32 }}>
                <svg width="18" height="18" viewBox="0 0 64 64" fill="none">
                  <path d="M22 44 L32 18 L42 44 M26 35 L38 35" stroke="#FF5500" strokeWidth="6" strokeLinecap="round" strokeLinejoin="round" />
                  <circle cx="32" cy="32" r="5" fill="#FFFFFF" />
                </svg>
              </div>
              <span>AR<span>Tracker</span></span>
            </div>

            <div className="status-badge">
              <span className="status-pulse" />
              <span>Core Ingest API: Operational (artracker.exlm.my.id)</span>
            </div>
          </div>

          <div className="footer-bottom">
            <div>© {CURRENT_YEAR} ARTracker Project. Built for high-performance product teams.</div>
            <div>Designed with Electric Orange & High-Fidelity Telemetry.</div>
          </div>
        </div>
      </footer>

      {/* SSO Login Modal */}
      {isSsoModalOpen && (
        <div 
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(5, 7, 10, 0.85)',
            backdropFilter: 'blur(8px)',
            zIndex: 1000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 20
          }}
          onClick={() => setIsSsoModalOpen(false)}
        >
          <div 
            style={{
              background: '#0F131D',
              border: '1px solid var(--border-highlight)',
              borderRadius: 'var(--radius-xl)',
              padding: '36px',
              maxWidth: '420px',
              width: '100%',
              boxShadow: '0 20px 50px rgba(0,0,0,0.8), 0 0 30px var(--primary-glow)'
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 }}>
              <div style={{ fontWeight: 800, fontSize: '1.25rem', color: '#FFF' }}>
                Sign In to <span style={{ color: 'var(--primary)' }}>ARTracker</span>
              </div>
              <button 
                onClick={() => setIsSsoModalOpen(false)}
                style={{ background: 'none', border: 'none', color: '#8E9BAE', fontSize: '1.2rem', cursor: 'pointer' }}
              >
                ✕
              </button>
            </div>

            <p style={{ fontSize: '0.88rem', color: '#9DA8BA', marginBottom: 24, lineHeight: 1.5 }}>
              Akses workspace monitoring kamu menggunakan akun Single Sign-On (SSO) perusahaan:
            </p>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <button 
                className="btn btn-secondary" 
                style={{ justifyContent: 'flex-start', padding: '12px 16px' }}
                onClick={() => {
                  alert('SSO Google Workspace integration stub ready for OAuth provider configuration.')
                  setIsSsoModalOpen(false)
                }}
              >
                <svg width="20" height="20" viewBox="0 0 24 24">
                  <path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"/>
                  <path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/>
                  <path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l2.85-2.22.81-.63z"/>
                  <path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.52 6.16-4.52z"/>
                </svg>
                <span style={{ marginLeft: 8 }}>Continue with Google Workspace</span>
              </button>

              <button 
                className="btn btn-secondary" 
                style={{ justifyContent: 'flex-start', padding: '12px 16px' }}
                onClick={() => {
                  alert('SSO GitHub Organization integration stub ready for OAuth provider configuration.')
                  setIsSsoModalOpen(false)
                }}
              >
                <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
                </svg>
                <span style={{ marginLeft: 8 }}>Continue with GitHub</span>
              </button>

              <button 
                className="btn btn-secondary" 
                style={{ justifyContent: 'flex-start', padding: '12px 16px' }}
                onClick={() => {
                  alert('SAML 2.0 / Okta enterprise identity provider redirect.')
                  setIsSsoModalOpen(false)
                }}
              >
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
                  <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
                </svg>
                <span style={{ marginLeft: 8 }}>SAML 2.0 / Okta / Custom OIDC</span>
              </button>
            </div>

            <div style={{ marginTop: 24, textAlign: 'center', fontSize: '0.78rem', color: '#626F84' }}>
              Protected by Enterprise Multi-Tenant Session Security
            </div>
          </div>
        </div>
      )}
    </>
  )
}
