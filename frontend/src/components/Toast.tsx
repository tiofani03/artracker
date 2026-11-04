import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'

type Kind = 'success' | 'error'
type Toast = { id: number; kind: Kind; message: string }
type Push = (message: string, kind?: Kind) => void

const ToastContext = createContext<Push>(() => {})
export const useToast = () => useContext(ToastContext)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const nextId = useRef(1)
  const region = useRef<HTMLDivElement>(null)

  const push = useCallback<Push>((message, kind = 'success') => {
    const id = nextId.current++
    setToasts((cur) => [...cur, { id, kind, message }])
    window.setTimeout(() => setToasts((cur) => cur.filter((t) => t.id !== id)), 3500)
  }, [])

  // A manual popover lives in the top layer, so toasts stay visible above open modals.
  useEffect(() => {
    const el = region.current
    if (!el || typeof el.showPopover !== 'function') return
    try {
      if (toasts.length > 0 && !el.matches(':popover-open')) el.showPopover()
      if (toasts.length === 0 && el.matches(':popover-open')) el.hidePopover()
    } catch {
      // popover unsupported: toasts still render, just below top-layer dialogs
    }
  }, [toasts])

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div ref={region} popover="manual" className="toast-region" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast--${t.kind}`}>
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}
