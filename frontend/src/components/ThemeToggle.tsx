import { useEffect, useState } from 'react'

export function useTheme() {
  const [theme, setThemeState] = useState<'light' | 'dark'>(() => {
    if (typeof window === 'undefined') return 'light'
    const stored = localStorage.getItem('color-scheme')
    if (stored === 'dark' || stored === 'light') return stored
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  })

  useEffect(() => {
    const applyTheme = (target: 'light' | 'dark') => {
      setThemeState(target)
      document.documentElement.dataset.theme = target
      const meta = document.querySelector('meta[name="color-scheme"]')
      if (meta) meta.setAttribute('content', target)
    }

    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const handleMediaChange = (e: MediaQueryListEvent) => {
      const stored = localStorage.getItem('color-scheme')
      if (!stored || stored === 'light dark') {
        applyTheme(e.matches ? 'dark' : 'light')
      }
    }

    const handleStorage = (e: StorageEvent) => {
      if (e.key === 'color-scheme') {
        const next = e.newValue
        if (next === 'dark' || next === 'light') {
          applyTheme(next)
        } else {
          applyTheme(mql.matches ? 'dark' : 'light')
        }
      }
    }

    mql.addEventListener('change', handleMediaChange)
    window.addEventListener('storage', handleStorage)
    return () => {
      mql.removeEventListener('change', handleMediaChange)
      window.removeEventListener('storage', handleStorage)
    }
  }, [])

  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark'
    try {
      localStorage.setItem('color-scheme', next)
    } catch {
      // ignore storage error
    }
    document.documentElement.dataset.theme = next
    const meta = document.querySelector('meta[name="color-scheme"]')
    if (meta) meta.setAttribute('content', next)
    setThemeState(next)
  }

  return { theme, toggleTheme }
}

export default function ThemeToggle({
  className,
  showLabel = true,
}: {
  className?: string
  showLabel?: boolean
}) {
  const { theme, toggleTheme } = useTheme()
  const isDark = theme === 'dark'

  return (
    <button
      type="button"
      className={`theme-toggle-btn ${className || ''}`}
      onClick={toggleTheme}
      title={isDark ? 'Switch to Light Mode' : 'Switch to Dark Mode'}
      aria-label={isDark ? 'Switch to Light Mode' : 'Switch to Dark Mode'}
    >
      {isDark ? (
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="5" />
          <line x1="12" y1="1" x2="12" y2="3" />
          <line x1="12" y1="21" x2="12" y2="23" />
          <line x1="4.22" y1="4.22" x2="5.64" y2="5.64" />
          <line x1="18.36" y1="18.36" x2="19.78" y2="19.78" />
          <line x1="1" y1="12" x2="3" y2="12" />
          <line x1="21" y1="12" x2="23" y2="12" />
          <line x1="4.22" y1="19.78" x2="5.64" y2="18.36" />
          <line x1="18.36" y1="5.64" x2="19.78" y2="4.22" />
        </svg>
      ) : (
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
        </svg>
      )}
      {showLabel && (
        <span className="theme-toggle-btn__label">{isDark ? 'Light' : 'Dark'}</span>
      )}
    </button>
  )
}
