import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'

export type PickerItem = { id: number; label: string; meta?: string }

type Props = {
  caption: string
  items: PickerItem[]
  activeId: number | null
  onSelect: (id: number) => void
  action?: { label: string; onClick: () => void }
  placeholder?: string
}

// Header dropdown used by both the Organization switcher and the Project selector.
export default function Picker({ caption, items, activeId, onSelect, action, placeholder = 'None' }: Props) {
  const [open, setOpen] = useState(false)
  const [hl, setHl] = useState(0)
  const root = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const uid = useId()
  const active = items.find((i) => i.id === activeId) ?? null
  const count = items.length + (action ? 1 : 0)

  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', away)
    list.current?.focus()
    return () => document.removeEventListener('mousedown', away)
  }, [open])

  const openMenu = () => {
    const at = items.findIndex((i) => i.id === activeId)
    setHl(at < 0 ? 0 : at)
    setOpen(true)
  }

  const choose = (index: number) => {
    setOpen(false)
    if (index < items.length) onSelect(items[index].id)
    else action?.onClick()
  }

  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      setOpen(false)
      root.current?.querySelector<HTMLButtonElement>('.picker__trigger')?.focus()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      setHl((h) => Math.min(h + 1, count - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHl((h) => Math.max(h - 1, 0))
    } else if (e.key === 'Home') {
      e.preventDefault()
      setHl(0)
    } else if (e.key === 'End') {
      e.preventDefault()
      setHl(count - 1)
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      if (count > 0) choose(hl)
    } else if (e.key === 'Tab') {
      setOpen(false)
    }
  }

  return (
    <div className="picker" ref={root}>
      <button
        type="button"
        className="picker__trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={`${caption}: ${active?.label ?? placeholder}`}
        onClick={() => (open ? setOpen(false) : openMenu())}
      >
        <span className="picker__text">
          <span className="picker__caption">{caption}</span>
          <span className="picker__value">{active?.label ?? placeholder}</span>
        </span>
        <svg className="picker__chevron" viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
          <path d="m7 10 5 5 5-5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {open && (
        <div
          ref={list}
          className="picker__menu"
          role="listbox"
          tabIndex={-1}
          aria-label={caption}
          aria-activedescendant={`${uid}-${hl}`}
          onKeyDown={onKey}
        >
          {items.map((item, i) => (
            <div
              key={item.id}
              id={`${uid}-${i}`}
              role="option"
              aria-selected={item.id === activeId}
              className={`picker__option${i === hl ? ' is-hl' : ''}${item.id === activeId ? ' is-active' : ''}`}
              onMouseEnter={() => setHl(i)}
              onClick={() => choose(i)}
            >
              <span className="picker__option-label">{item.label}</span>
              {item.meta && <span className="picker__meta">{item.meta}</span>}
            </div>
          ))}
          {items.length === 0 && <div className="picker__empty">Nothing here yet</div>}
          {action && (
            <div
              id={`${uid}-${items.length}`}
              role="option"
              aria-selected="false"
              className={`picker__option picker__option--action${hl === items.length ? ' is-hl' : ''}`}
              onMouseEnter={() => setHl(items.length)}
              onClick={() => choose(items.length)}
            >
              <span aria-hidden="true">+</span> {action.label}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
