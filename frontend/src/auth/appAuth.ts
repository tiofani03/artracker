import { BASE, setAccessToken } from '../api'
import { navigate } from '../router'
import { getStored, setRefreshToken, setStored } from './appStore'

const CALLBACK_HOST = 'auth'
const CALLBACK_PATH = '/callback'

function b64url(bytes: Uint8Array): string {
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

async function pkce(): Promise<{ verifier: string; challenge: string }> {
  const verifier = b64url(crypto.getRandomValues(new Uint8Array(48)))
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  return { verifier, challenge: b64url(new Uint8Array(digest)) }
}

/** Opens Google sign-in in the system browser; the result returns through artracker://. */
export async function startAppLogin(): Promise<void> {
  const { verifier, challenge } = await pkce()
  await setStored('pkce_verifier', verifier)
  const { openUrl } = await import('@tauri-apps/plugin-opener')
  await openUrl(`${BASE}/auth/google/login?client=app&code_challenge=${challenge}`)
}

async function exchange(code: string): Promise<boolean> {
  const verifier = await getStored('pkce_verifier')
  if (!verifier) return false
  const res = await fetch(`${BASE}/auth/app/exchange`, {
    method: 'POST',
    credentials: 'omit',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code, code_verifier: verifier }),
  })
  if (!res.ok) return false
  const body = (await res.json()) as { access_token: string; refresh_token: string }
  setAccessToken(body.access_token)
  await setRefreshToken(body.refresh_token)
  await setStored('pkce_verifier', null)
  return true
}

async function handleUrl(raw: string, onSignedIn: () => Promise<void>): Promise<void> {
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return
  }
  if (u.protocol !== 'artracker:' || u.host !== CALLBACK_HOST || u.pathname !== CALLBACK_PATH) return

  const error = u.searchParams.get('error')
  const code = u.searchParams.get('code')
  if (error || !code) {
    navigate(`/login?error=${encodeURIComponent(error ?? 'provider_failed')}`, true)
    return
  }
  if (await exchange(code)) {
    await onSignedIn()
    navigate('/dashboard', true)
  } else {
    navigate('/login?error=invalid_state', true)
  }
}

let started = false
const seen = new Set<string>()

/** Listens for artracker:// links (cold start and while running). Call once. */
export async function initAppDeepLinks(onSignedIn: () => Promise<void>): Promise<void> {
  if (started) return
  started = true
  const { getCurrent, onOpenUrl } = await import('@tauri-apps/plugin-deep-link')
  const handle = (urls: string[]) => {
    for (const url of urls) {
      if (seen.has(url)) continue // a one-time code must not be redeemed twice
      seen.add(url)
      void handleUrl(url, onSignedIn)
    }
  }
  handle((await getCurrent()) ?? [])
  await onOpenUrl(handle)
}

/** Opens external http(s) links in the system browser instead of navigating the app webview. */
export function installExternalLinkHandler(): void {
  document.addEventListener('click', (e) => {
    const a = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null
    if (!a || e.defaultPrevented) return
    const href = a.getAttribute('href') ?? ''
    if (!/^https?:\/\//i.test(href)) return
    e.preventDefault()
    void import('@tauri-apps/plugin-opener').then(({ openUrl }) => openUrl(href))
  })
}
