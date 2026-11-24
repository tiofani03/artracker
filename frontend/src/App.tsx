import { useEffect, useRef } from 'react'
import { BASE } from './api'
import { useAuth } from './auth/AuthContext'
import { useAuthConfig } from './auth/useAuthConfig'
import GoogleIcon from './components/GoogleIcon'
import ThemeToggle from './components/ThemeToggle'
import { Link, navigate } from './router'

declare global {
  interface Window {
    ScrollCraft?: {
      mount: (root?: Document | HTMLElement) => {
        layout: () => void
        read: () => void
        destroy: () => void
      }
    }
  }
}

const START_STEPS = [
  {
    index: '01',
    title: 'Deploy',
    body: 'Run Postgres, the Go API, and the dashboard together with one Docker Compose command. Migrations run on their own.',
    note: 'Self-hosted · your data stays on your server',
  },
  {
    index: '02',
    title: 'Create a project',
    body: 'Sign in, create a project, and copy its public key. Add it to your app and it is ready to report.',
    note: 'Public keys are safe to ship in client apps',
  },
  {
    index: '03',
    title: 'Watch it arrive',
    body: 'Sessions, crashes, and logs show up in the dashboard. Errors are grouped into issues for you.',
    note: 'Replays, issues, and alerts in one place',
  },
]

const ENGINES = [
  {
    index: '01',
    title: 'Session replay',
    span: '7' as const,
    body: 'Reconstructs DOM mutations, pointer velocity, and rage clicks to the millisecond.',
    spec: 'rrweb stream · mobile trees · client PII redaction',
    icon: 'M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2zm-2 14.5v-9l6 4.5z',
  },
  {
    index: '02',
    title: 'Crash & trace log',
    span: '5' as const,
    body: 'Unfolds minified stack traces beside the exact network call before the crash.',
    spec: 'Source maps · OkHttp & URLSession spans',
    icon: 'M4 6h16M4 12h16M4 18h10',
  },
  {
    index: '03',
    title: 'Conversion funnels',
    span: '5' as const,
    body: 'Links drop-off steps directly to the recorded sessions where users stalled.',
    spec: 'UTM attribution · session-linked cohorts',
    icon: 'M3 4h18l-7 8v6l-4 2v-8z',
  },
  {
    index: '04',
    title: 'Latency boards',
    span: '7' as const,
    body: 'Charts crash-free ratios and p50/p95/p99 endpoint latency on live SQL boards.',
    spec: 'p95 & p99 histograms · release health',
    icon: 'M3 3v18h18M7 15l4-4 3 3 5-6',
  },
  {
    index: '05',
    title: 'Enterprise SSO',
    span: '6' as const,
    body: 'Authenticates teams via Google, GitHub, Okta, and SAML 2.0 with scoped RBAC.',
    spec: 'OIDC · SAML 2.0 · role permissions',
    icon: 'M12 11a4 4 0 1 0-4-4 4 4 0 0 0 4 4zm-7 9a7 7 0 0 1 14 0',
  },
  {
    index: '06',
    title: 'Direct alerts',
    span: '6' as const,
    body: 'Routes incident payloads to Telegram, Slack, and PagerDuty when errors spike.',
    spec: 'Telegram bot · Slack webhooks · thresholds',
    icon: 'M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 0 1-3.46 0',
  },
]

function DaylightSessionStage() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const wrapRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    const wrap = wrapRef.current
    if (!canvas || !wrap) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const glyphs = ['·', ':', '+', '=', '#']

    let c1 = ''
    let c2 = ''
    let c3 = ''
    let cAccent = ''

    const syncColors = () => {
      const s = getComputedStyle(document.documentElement)
      c1 = s.getPropertyValue('--color-glyph-1').trim() || c1
      c2 = s.getPropertyValue('--color-glyph-2').trim() || c2
      c3 = s.getPropertyValue('--color-glyph-3').trim() || c3
      cAccent = s.getPropertyValue('--color-accent').trim() || cAccent
    }
    syncColors()

    let width = 0
    let height = 0
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    let pointerX = -999
    let pointerY = -999
    let phase = 0
    let inView = true
    let rafId = 0
    let lastPaint = 0

    const draw = () => {
      if (!c1) syncColors()
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, width, height)
      ctx.font = '11px "JetBrains Mono", ui-monospace, monospace'
      ctx.textBaseline = 'middle'
      ctx.textAlign = 'center'

      const cellW = 16
      const cellH = 20
      const cols = Math.ceil(width / cellW)
      const rows = Math.ceil(height / cellH)

      for (let r = 0; r < rows; r++) {
        const y = r * cellH + 10
        for (let c = 0; c < cols; c++) {
          const x = c * cellW + 8
          const wave =
            Math.sin(c * 0.22 - phase) * 0.5 +
            Math.cos(r * 0.28 + phase * 0.6) * 0.5
          const dx = x - pointerX
          const dy = y - pointerY
          const dist = Math.hypot(dx, dy)
          const lens = dist < 96 ? (96 - dist) / 96 : 0
          const val = (wave + 1) * 0.5 + lens * 0.45

          let idx = 0
          if (val > 1.05) {
            idx = 4
            ctx.fillStyle = cAccent
          } else if (val > 0.78) {
            idx = 3
            ctx.fillStyle = c3
          } else if (val > 0.55) {
            idx = 2
            ctx.fillStyle = c2
          } else {
            idx = val > 0.3 ? 1 : 0
            ctx.fillStyle = c1
          }

          ctx.fillText(glyphs[idx], x, y)
        }
      }
    }

    const resize = () => {
      const rect = wrap.getBoundingClientRect()
      width = Math.max(rect.width, 260)
      height = Math.max(rect.height, 260)
      canvas.width = Math.round(width * dpr)
      canvas.height = Math.round(height * dpr)
      draw()
    }
    resize()

    const tick = (now: number) => {
      if (inView && !reducedMotion && now - lastPaint > 33) {
        phase += 0.045
        lastPaint = now
        draw()
      }
      if (!reducedMotion) {
        rafId = window.requestAnimationFrame(tick)
      }
    }

    if (!reducedMotion) {
      rafId = window.requestAnimationFrame(tick)
    }

    const ro = new ResizeObserver(() => resize())
    ro.observe(wrap)

    const io = new IntersectionObserver(
      ([entry]) => {
        inView = entry.isIntersecting
      },
      { threshold: 0.05 },
    )
    io.observe(wrap)

    const onMove = (e: PointerEvent) => {
      const r = wrap.getBoundingClientRect()
      pointerX = e.clientX - r.left
      pointerY = e.clientY - r.top
    }
    const onLeave = () => {
      pointerX = -999
      pointerY = -999
    }

    wrap.addEventListener('pointermove', onMove, { passive: true })
    wrap.addEventListener('pointerleave', onLeave)

    return () => {
      window.cancelAnimationFrame(rafId)
      ro.disconnect()
      io.disconnect()
      wrap.removeEventListener('pointermove', onMove)
      wrap.removeEventListener('pointerleave', onLeave)
    }
  }, [])

  return (
    <div ref={wrapRef} className="stage-visual">
      <div className="glyph-backdrop" aria-hidden="true">
        <canvas ref={canvasRef} className="glyph-canvas" />
      </div>

      <figure className="inspector-sheet cut" data-kind="product" data-sc-parallax="-0.12">
        <div className="inspector-sheet__top">
          <div className="inspector-sheet__id">
            <span className="state-dot state-dot--accent" aria-hidden="true" />
            <span>session rec_9082fd11</span>
          </div>
          <span className="inspector-sheet__meta">Singapore · Chrome 131</span>
        </div>

        <div className="inspector-sheet__body">
          <div className="inspector-summary">
            <div className="inspector-stat">
              <span className="inspector-stat__val">99.94%</span>
              <span className="inspector-stat__unit">Crash-free sessions</span>
            </div>
            <div className="inspector-stat">
              <span className="inspector-stat__val">18 ms</span>
              <span className="inspector-stat__unit">Ingest p95</span>
            </div>
            <div className="inspector-stat">
              <span className="inspector-stat__val">0 B</span>
              <span className="inspector-stat__unit">Unmasked PII</span>
            </div>
          </div>

          <div className="inspector-rows">
            <div className="inspector-row">
              <span className="inspector-row__time">00:08</span>
              <span className="inspector-row__state">
                <span className="state-dot state-dot--ok" aria-hidden="true" />
                Redacted
              </span>
              <span className="inspector-row__detail">
                input#card-number masked on client
              </span>
            </div>

            <div className="inspector-row">
              <span className="inspector-row__time">00:30</span>
              <span className="inspector-row__state">
                <span className="state-dot state-dot--warn" aria-hidden="true" />
                Rage click
              </span>
              <span className="inspector-row__detail">
                4 taps on button#pay within 400 ms
              </span>
            </div>

            <div className="inspector-row inspector-row--active">
              <span className="inspector-row__time">00:46</span>
              <span className="inspector-row__state">
                <span className="state-dot state-dot--err" aria-hidden="true" />
                HTTP 502
              </span>
              <span className="inspector-row__detail">
                POST /api/v1/charge · PaymentForm.tsx:142
              </span>
            </div>

            <div className="inspector-row">
              <span className="inspector-row__time">00:47</span>
              <span className="inspector-row__state">
                <span className="state-dot state-dot--accent" aria-hidden="true" />
                Alert sent
              </span>
              <span className="inspector-row__detail">
                Dispatched to #ops-payments webhook
              </span>
            </div>
          </div>
        </div>

        <div className="inspector-sheet__foot">
          <span>Correlated across DOM, network, and source maps</span>
          <span>MIT self-hosted</span>
        </div>
      </figure>
    </div>
  )
}

export default function App() {
  const { user } = useAuth()
  const authConfig = useAuthConfig()
  const scrollBarRef = useRef<HTMLDivElement | null>(null)
  const dialogRef = useRef<HTMLDialogElement | null>(null)


  // Mount untouched ScrollCraft engine for smooth entrance choreography
  useEffect(() => {
    let instance: { layout: () => void; read: () => void; destroy: () => void } | null = null
    let attempts = 0
    let timer = 0

    const tryMount = () => {
      if (window.ScrollCraft) {
        instance = window.ScrollCraft.mount(document)
        return
      }
      attempts += 1
      if (attempts < 25) {
        timer = window.setTimeout(tryMount, 60)
      }
    }
    tryMount()

    return () => {
      if (timer) window.clearTimeout(timer)
      instance?.destroy()
    }
  }, [])

  // Fallback scroll progress bar for browsers without CSS scroll-timeline
  useEffect(() => {
    const supportsScrollTimeline =
      typeof CSS !== 'undefined' && CSS.supports('animation-timeline', 'scroll(root)')
    if (supportsScrollTimeline) return

    const onScroll = () => {
      const scrollable = Math.max(document.documentElement.scrollHeight - window.innerHeight, 1)
      const ratio = Math.min(1, Math.max(0, window.scrollY / scrollable))
      if (scrollBarRef.current) {
        scrollBarRef.current.style.transform = `scaleX(${ratio.toFixed(4)})`
      }
    }

    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  const openSsoModal = () => {
    if (user) {
      navigate('/dashboard')
      return
    }
    dialogRef.current?.showModal()
  }

  const closeSsoModal = () => {
    dialogRef.current?.close()
  }


  return (
    <>
      <a href="#main-content" className="skip-link">
        Skip to main content
      </a>

      {/* Subtle top scroll progress hairline */}
      <div className="scroll-hairline" aria-hidden="true">
        <div ref={scrollBarRef} className="scroll-hairline__bar" />
      </div>

      {/* Clean Daylight Header */}
      <header className="site-header">
        <div className="container site-header__inner">
          <a href="#top" className="wordmark">
            <svg className="wordmark__glyph" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path
                d="M4 19L12 4L20 19M7.5 14H16.5"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="square"
              />
            </svg>
            <span>ARTracker</span>
          </a>

          <nav className="site-nav" aria-label="Primary">
            <a href="#features" className="site-nav__link">
              Features
            </a>
            <a href="#get-started" className="site-nav__link">
              Get started
            </a>
            <a
              href="/docs"
              onClick={(e) => {
                e.preventDefault()
                navigate('/docs')
              }}
              className="site-nav__link"
            >
              Documentation
            </a>
            <a
              href="https://github.com/tiofani03/artracker"
              target="_blank"
              rel="noreferrer"
              className="site-nav__link"
            >
              GitHub
            </a>
          </nav>

          <div className="header-tools">
            <ThemeToggle showLabel={false} />
            <button type="button" className="key key--primary" onClick={openSsoModal}>
              {user ? 'Dashboard' : 'Login'}
            </button>
          </div>
        </div>
      </header>

      <main id="main-content">
        {/* Section 1: Clean Daylight Stage Hero */}
        <section id="top" className="hero-stage" data-sc-act="flow">
          <div className="container stage-split">
            <div className="stage-copy">
              <h1>Every broken session, replayed to the frame.</h1>
              <p className="stage-lead">
                Self-hosted session replay, crash traces, and funnels in one Go binary.
              </p>
              <div className="stage-actions">
                <button
                  type="button"
                  className="key key--primary"
                  onClick={openSsoModal}
                >
                  {user ? 'Open Dashboard' : 'Login'}
                </button>
                <a
                  href="/docs"
                  onClick={(e) => {
                    e.preventDefault()
                    navigate('/docs')
                  }}
                  className="key"
                >
                  API Docs &amp; Playground
                </a>
              </div>
            </div>

            <DaylightSessionStage />
          </div>
        </section>

        {/* Section 2: Core Capabilities (6 Engines) */}
        <section id="features" className="engines-section">
          <div className="container">
            <div className="engines-head" data-sc-in>
              <h2>Everything needed to debug production.</h2>
              <p className="engines-head__desc">
                Replay, crash diagnostics, funnels, and alerting ship together in one open-source
                stack.
              </p>
            </div>

            <div className="engines-bento" data-sc-in data-sc-stagger="50">
              {ENGINES.map((engine) => (
                <article
                  key={engine.index}
                  className={`engine-card engine-card--${engine.span} cut`}
                >
                  <div className="engine-card__top">
                    <div className="engine-card__header">
                      <span className="engine-card__index">{engine.index}</span>
                      <svg
                        className="engine-card__icon"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.75"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        aria-hidden="true"
                      >
                        <path d={engine.icon} />
                      </svg>
                    </div>
                    <h3>{engine.title}</h3>
                    <p className="engine-card__body">{engine.body}</p>
                  </div>
                  <div className="engine-card__spec">{engine.spec}</div>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* Section 3: Get started in three steps */}
        <section id="get-started" className="sdk-section">
          <div className="container">
            <div className="sdk-header" data-sc-in>
              <h2>Up and running in three steps.</h2>
              <p className="sdk-header__desc">
                Self-host with Docker, create a project, and start seeing what your users see.
              </p>
            </div>

            <ol className="steps-grid">
              {START_STEPS.map((step) => (
                <li key={step.index} className="step-card cut" data-sc-in>
                  <span className="step-card__index">{step.index}</span>
                  <h3>{step.title}</h3>
                  <p className="step-card__body">{step.body}</p>
                  <p className="step-card__note">{step.note}</p>
                </li>
              ))}
            </ol>

            <p className="steps-docs">
              Need setup details?{' '}
              <a
                href="/docs"
                onClick={(e) => {
                  e.preventDefault()
                  navigate('/docs')
                }}
              >
                Read the documentation &rarr;
              </a>
            </p>
          </div>
        </section>

        {/* Section 4: Clean Closing CTA */}
        <section id="start" className="close-section">
          <div className="container">
            <div className="close-card cut" data-sc-in>
              <div className="close-copy">
                <h2>Start capturing sessions in minutes.</h2>
                <p className="close-copy__desc">
                  Deploy on your own server, then trace every issue back to the exact user action.
                </p>
              </div>

              <div className="close-actions">
                <button
                  type="button"
                  className="key key--primary"
                  onClick={openSsoModal}
                >
                  {user ? 'Open Dashboard' : 'Login'}
                </button>
                <a
                  href="/docs"
                  onClick={(e) => {
                    e.preventDefault()
                    navigate('/docs')
                  }}
                  className="key"
                >
                  API Documentation
                </a>
              </div>
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="site-footer">
        <div className="container site-footer__inner">
          <span>ARTracker v0.2.0</span>
          <div className="footer-links">
            <a href="#features">Features</a>
            <a href="#get-started">Get started</a>
            <a
              href="/docs"
              onClick={(e) => {
                e.preventDefault()
                navigate('/docs')
              }}
            >
              Documentation
            </a>
            <a href="https://github.com/tiofani03/artracker" target="_blank" rel="noreferrer">
              GitHub
            </a>
          </div>
          <span>© {new Date().getFullYear()} ARTracker</span>
        </div>
      </footer>

      {/* Native <dialog> Login Modal */}
      <dialog
        ref={dialogRef}
        className="sso-dialog"
        aria-labelledby="sso-dialog-title"
        onClick={(e) => {
          if (e.target === dialogRef.current) closeSsoModal()
        }}
      >
        <div className="sso-dialog__hero">
          <div className="sso-dialog__bar">
            <div className="sso-brand">
              <span className="sso-logo-badge" aria-hidden="true">
                <svg className="sso-logo-badge__svg" viewBox="0 0 24 24" fill="none">
                  <path
                    d="M4 19L12 4L20 19M7.5 14H16.5"
                    stroke="currentColor"
                    strokeWidth="2.4"
                    strokeLinecap="square"
                  />
                </svg>
              </span>
              <span className="sso-brand__meta">
                <span className="sso-brand__name">ARTracker</span>
                <span className="sso-brand__tag">Telemetry Console</span>
              </span>
            </div>

            <button
              type="button"
              className="sso-dialog__close"
              aria-label="Close login modal"
              onClick={closeSsoModal}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path
                  d="M6 6l12 12M18 6L6 18"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                />
              </svg>
            </button>
          </div>

          <div className="sso-dialog__intro">
            <h3 id="sso-dialog-title" className="sso-dialog__title">
              Login to ARTracker
            </h3>
            <p className="sso-dialog__sub">
              Continue to your workspace to inspect sessions, crashes, and funnels.
            </p>
          </div>
        </div>

        <div className="sso-dialog__body">
          <div className="sso-providers">
            {authConfig.google && (
              <a className="sso-provider-btn" href={`${BASE}/auth/google/login`}>
                <span className="sso-provider-btn__left">
                  <span className="sso-provider-btn__icon" aria-hidden="true">
                    <GoogleIcon size={16} />
                  </span>
                  <span>Continue with Google</span>
                </span>
                <span className="sso-provider-btn__right">
                  <svg className="sso-provider-btn__arrow" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                    <path d="M5 12h14m-5-5 5 5-5 5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </span>
              </a>
            )}
            {authConfig.local && (
              <Link className="sso-provider-btn" to="/login" onClick={closeSsoModal}>
                <span className="sso-provider-btn__left">
                  <span>Sign in with email</span>
                </span>
                <span className="sso-provider-btn__right">
                  <svg className="sso-provider-btn__arrow" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                    <path d="M5 12h14m-5-5 5 5-5 5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </span>
              </Link>
            )}
          </div>
          <p className="sso-note">
            {authConfig.local && authConfig.google
              ? 'Sign in with Google or your invited email account.'
              : authConfig.local
                ? 'Sign in with your invited email and password.'
                : 'Google is the only supported sign-in method.'}
          </p>
        </div>
      </dialog>
    </>
  )
}
