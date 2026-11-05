import {
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'

export type CustomSelectOption<T extends string | number = string> = {
  value: T
  label: string
  meta?: string
  badge?: string
  disabled?: boolean
}

type Props<T extends string | number = string> = {
  id?: string
  label?: string
  options: CustomSelectOption<T>[]
  value: T
  onChange: (value: T) => void
  placeholder?: string
  disabled?: boolean
  size?: 'sm' | 'md'
  searchable?: boolean
  className?: string
  ariaLabel?: string
  hint?: ReactNode
}

export default function CustomSelect<T extends string | number = string>({
  id,
  label,
  options,
  value,
  onChange,
  placeholder = 'Select an option',
  disabled = false,
  size = 'md',
  searchable = false,
  className = '',
  ariaLabel,
  hint,
}: Props<T>) {
  const [isOpen, setIsOpen] = useState(false)
  const [highlightIdx, setHighlightIdx] = useState<number>(-1)
  const [searchQuery, setSearchQuery] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const listboxRef = useRef<HTMLUListElement>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const generatedId = useId()
  const selectId = id || generatedId

  // Filter options if searchable is enabled
  const filteredOptions = searchable && searchQuery.trim()
    ? options.filter(
        (opt) =>
          opt.label.toLowerCase().includes(searchQuery.toLowerCase()) ||
          opt.meta?.toLowerCase().includes(searchQuery.toLowerCase()) ||
          opt.badge?.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    : options

  const currentOption = options.find((opt) => opt.value === value)

  // Click outside listener
  useEffect(() => {
    if (!isOpen) return
    const handleClickOutside = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) {
        setIsOpen(false)
        setSearchQuery('')
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [isOpen])

  // Focus management when menu opens
  useEffect(() => {
    if (isOpen) {
      const activeIdx = filteredOptions.findIndex((opt) => opt.value === value)
      setHighlightIdx(activeIdx >= 0 ? activeIdx : 0)
      if (searchable && searchInputRef.current) {
        searchInputRef.current.focus()
      } else if (listboxRef.current) {
        listboxRef.current.focus()
      }
    } else {
      setSearchQuery('')
    }
  }, [isOpen, value, searchable, filteredOptions])

  // Scroll active item into view
  useEffect(() => {
    if (!isOpen || highlightIdx < 0 || !listboxRef.current) return
    const activeItem = listboxRef.current.children[highlightIdx] as HTMLElement | undefined
    if (activeItem) {
      activeItem.scrollIntoView({ block: 'nearest' })
    }
  }, [highlightIdx, isOpen])

  const handleSelect = (opt: CustomSelectOption<T>) => {
    if (opt.disabled) return
    onChange(opt.value)
    setIsOpen(false)
    setSearchQuery('')
    rootRef.current?.querySelector<HTMLButtonElement>('.cselect__trigger')?.focus()
  }

  const handleTriggerClick = () => {
    if (disabled) return
    setIsOpen((prev) => !prev)
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (disabled) return

    if (!isOpen) {
      if (['Enter', ' ', 'ArrowDown', 'ArrowUp'].includes(e.key)) {
        e.preventDefault()
        setIsOpen(true)
      }
      return
    }

    switch (e.key) {
      case 'Escape':
        e.preventDefault()
        setIsOpen(false)
        setSearchQuery('')
        rootRef.current?.querySelector<HTMLButtonElement>('.cselect__trigger')?.focus()
        break

      case 'ArrowDown':
        e.preventDefault()
        setHighlightIdx((prev) => {
          let next = prev + 1
          while (next < filteredOptions.length && filteredOptions[next]?.disabled) {
            next++
          }
          return next < filteredOptions.length ? next : prev
        })
        break

      case 'ArrowUp':
        e.preventDefault()
        setHighlightIdx((prev) => {
          let next = prev - 1
          while (next >= 0 && filteredOptions[next]?.disabled) {
            next--
          }
          return next >= 0 ? next : prev
        })
        break

      case 'Home':
        e.preventDefault()
        setHighlightIdx(0)
        break

      case 'End':
        e.preventDefault()
        setHighlightIdx(filteredOptions.length - 1)
        break

      case 'Enter':
      case ' ':
        // When searchable input is focused and typing space, don't trigger selection
        if (e.key === ' ' && document.activeElement === searchInputRef.current) {
          return
        }
        e.preventDefault()
        if (highlightIdx >= 0 && highlightIdx < filteredOptions.length) {
          const selected = filteredOptions[highlightIdx]
          if (selected && !selected.disabled) {
            handleSelect(selected)
          }
        }
        break

      case 'Tab':
        setIsOpen(false)
        setSearchQuery('')
        break
    }
  }

  return (
    <div
      ref={rootRef}
      className={`cselect cselect--${size}${disabled ? ' is-disabled' : ''}${isOpen ? ' is-open' : ''} ${className}`.trim()}
      onKeyDown={handleKeyDown}
    >
      {label && (
        <label htmlFor={selectId} className="cselect__label">
          {label}
        </label>
      )}

      <button
        id={selectId}
        type="button"
        className="cselect__trigger"
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-label={ariaLabel || label || currentOption?.label || placeholder}
        disabled={disabled}
        onClick={handleTriggerClick}
      >
        <span className="cselect__trigger-content">
          {currentOption ? (
            <span className="cselect__selected-wrap">
              <span className="cselect__selected-text">{currentOption.label}</span>
              {currentOption.badge && (
                <span className="cselect__badge">{currentOption.badge}</span>
              )}
            </span>
          ) : (
            <span className="cselect__placeholder">{placeholder}</span>
          )}
        </span>
        <span className="cselect__state-indicator" aria-hidden="true">
          {isOpen ? 'Close' : 'Select'}
        </span>
      </button>

      {isOpen && (
        <div className="cselect__dropdown">
          {searchable && (
            <div className="cselect__search-wrap">
              <input
                ref={searchInputRef}
                type="text"
                className="cselect__search-input"
                placeholder="Search options..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onClick={(e) => e.stopPropagation()}
                aria-label="Filter options"
              />
            </div>
          )}

          <ul
            ref={listboxRef}
            className="cselect__listbox"
            role="listbox"
            tabIndex={-1}
            aria-label={ariaLabel || label || 'Options'}
            aria-activedescendant={
              highlightIdx >= 0 ? `${selectId}-opt-${highlightIdx}` : undefined
            }
          >
            {filteredOptions.length === 0 ? (
              <li className="cselect__empty">No matching options</li>
            ) : (
              filteredOptions.map((opt, idx) => {
                const isSelected = opt.value === value
                const isHighlighted = idx === highlightIdx

                return (
                  <li
                    key={String(opt.value)}
                    id={`${selectId}-opt-${idx}`}
                    role="option"
                    aria-selected={isSelected}
                    aria-disabled={opt.disabled}
                    className={`cselect__option${isSelected ? ' is-selected' : ''}${isHighlighted ? ' is-highlighted' : ''}${opt.disabled ? ' is-disabled' : ''}`}
                    onClick={() => handleSelect(opt)}
                    onMouseEnter={() => !opt.disabled && setHighlightIdx(idx)}
                  >
                    <div className="cselect__option-main">
                      <span className="cselect__option-label">{opt.label}</span>
                      {opt.badge && <span className="cselect__badge">{opt.badge}</span>}
                    </div>
                    {opt.meta && <span className="cselect__option-meta">{opt.meta}</span>}
                    {isSelected && (
                      <span className="cselect__selected-tag" aria-hidden="true">
                        Active
                      </span>
                    )}
                  </li>
                )
              })
            )}
          </ul>
        </div>
      )}

      {hint && <span className="cselect__hint">{hint}</span>}
    </div>
  )
}
