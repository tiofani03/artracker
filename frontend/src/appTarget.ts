// Build flavour: `vite build --mode app` produces the Tauri app (login screen onward,
// no landing page, no demo mode, talks to a remote API with bearer tokens only).
export const IS_APP = import.meta.env.VITE_TARGET === 'app'

/** Origin of the API server for absolute URLs; '' on the web where paths stay same-origin. */
export const API_ORIGIN: string = (() => {
  try {
    return IS_APP ? new URL(import.meta.env.VITE_API_BASE ?? '').origin : ''
  } catch {
    return ''
  }
})()
