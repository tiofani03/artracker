import { useEffect, useId, useRef, type ReactNode } from 'react'

type Props = {
  open: boolean
  onClose: () => void
  title: string
  description?: string
  children: ReactNode
}

// Native <dialog> modal built on main's .sso-dialog chrome. Children only mount while
// open, so forms inside always start from a clean state.
export default function Modal({ open, onClose, title, description, children }: Props) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  }, [open])

  return (
    <dialog
      ref={ref}
      className="sso-dialog dash-modal"
      aria-labelledby={titleId}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose()
      }}
    >
      {open && (
        <>
          <header className="dash-modal__head">
            <div>
              <h2 id={titleId} className="dash-modal__title">
                {title}
              </h2>
              {description && <p className="dash-modal__desc">{description}</p>}
            </div>
            <button type="button" className="dash-icon-btn" aria-label="Close" onClick={onClose}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
                <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
              </svg>
            </button>
          </header>
          <div className="dash-modal__body">{children}</div>
        </>
      )}
    </dialog>
  )
}
