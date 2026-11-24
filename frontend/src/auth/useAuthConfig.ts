import { useEffect, useState } from 'react'
import { authApi, type AuthConfig } from '../api'

const FALLBACK: AuthConfig = { google: true, local: false }

let cached: AuthConfig | null = null
let inflight: Promise<AuthConfig> | null = null

function loadAuthConfig(): Promise<AuthConfig> {
  if (cached) return Promise.resolve(cached)
  inflight ??= authApi
    .config()
    .then((cfg) => {
      cached = cfg
      return cfg
    })
    .catch(() => {
      cached = FALLBACK
      return FALLBACK
    })
    .finally(() => {
      inflight = null
    })
  return inflight
}

export function useAuthConfig(): { google: boolean; local: boolean; loading: boolean } {
  const [config, setConfig] = useState<AuthConfig>(cached ?? FALLBACK)
  const [loading, setLoading] = useState(cached === null)

  useEffect(() => {
    if (cached) return
    let cancelled = false
    void loadAuthConfig().then((cfg) => {
      if (cancelled) return
      setConfig(cfg)
      setLoading(false)
    })
    return () => {
      cancelled = true
    }
  }, [])

  return { ...config, loading }
}
