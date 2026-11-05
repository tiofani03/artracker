import { useState, type DragEvent, type FormEvent } from 'react'
import type { EventCatalog, SavedFunnel } from '../../api'

type Props = {
  steps: string[]
  onStepsChange: (steps: string[]) => void
  windowSeconds: number
  onWindowChange: (secs: number) => void
  countingMode: 'users' | 'sessions'
  onCountingModeChange: (mode: 'users' | 'sessions') => void
  breakdownBy: string
  onBreakdownChange: (val: string) => void
  catalog?: EventCatalog | null
  savedFunnels: SavedFunnel[]
  onSelectSavedFunnel: (funnel: SavedFunnel) => void
  onSaveCurrentFunnel: (name: string) => Promise<void>
  onRunQuery: () => void
  loading: boolean
}

export default function FunnelBuilder({
  steps,
  onStepsChange,
  windowSeconds,
  onWindowChange,
  countingMode,
  onCountingModeChange,
  breakdownBy,
  onBreakdownChange,
  catalog,
  savedFunnels,
  onSelectSavedFunnel,
  onSaveCurrentFunnel,
  onRunQuery,
  loading,
}: Props) {
  const [saveModalOpen, setSaveModalOpen] = useState(false)
  const [newFunnelName, setNewFunnelName] = useState('')
  const [saving, setSaving] = useState(false)
  const [draggedIdx, setDraggedIdx] = useState<number | null>(null)

  const handleStepChange = (idx: number, val: string) => {
    const updated = [...steps]
    updated[idx] = val
    onStepsChange(updated)
  }

  const handleAddStep = () => {
    if (steps.length >= 5) return
    onStepsChange([...steps, ''])
  }

  const handleRemoveStep = (idx: number) => {
    if (steps.length <= 2) return
    const updated = steps.filter((_, i) => i !== idx)
    onStepsChange(updated)
  }

  const handleMoveStep = (fromIdx: number, toIdx: number) => {
    if (toIdx < 0 || toIdx >= steps.length) return
    const updated = [...steps]
    const [moved] = updated.splice(fromIdx, 1)
    updated.splice(toIdx, 0, moved)
    onStepsChange(updated)
  }

  // HTML5 Drag and Drop handlers
  const onDragStart = (e: DragEvent<HTMLDivElement>, idx: number) => {
    setDraggedIdx(idx)
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(idx))
  }

  const onDragOver = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
  }

  const onDrop = (e: DragEvent<HTMLDivElement>, targetIdx: number) => {
    e.preventDefault()
    if (draggedIdx === null || draggedIdx === targetIdx) return
    handleMoveStep(draggedIdx, targetIdx)
    setDraggedIdx(null)
  }

  const handleSaveSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!newFunnelName.trim()) return
    setSaving(true)
    try {
      await onSaveCurrentFunnel(newFunnelName.trim())
      setNewFunnelName('')
      setSaveModalOpen(false)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="funnel-builder-panel">
      {/* Top Action Row: Saved Presets & Options */}
      <div className="funnel-builder-top">
        <div className="funnel-builder-presets">
          {savedFunnels.length > 0 && (
            <div className="dash-select-wrap">
              <label htmlFor="saved-funnel-select" className="funnel-ctrl-label">
                Saved Preset:
              </label>
              <select
                id="saved-funnel-select"
                className="dash-select"
                onChange={(e) => {
                  const f = savedFunnels.find((item) => item.id === Number(e.target.value))
                  if (f) onSelectSavedFunnel(f)
                }}
                defaultValue=""
              >
                <option value="" disabled>
                  Load a saved funnel...
                </option>
                {savedFunnels.map((sf) => (
                  <option key={sf.id} value={sf.id}>
                    {sf.name} ({sf.steps.length} steps)
                  </option>
                ))}
              </select>
            </div>
          )}

          <button
            type="button"
            className="dash-btn dash-btn--sm"
            onClick={() => setSaveModalOpen(true)}
            title="Save current steps as team preset"
          >
            💾 Save Preset
          </button>
        </div>

        <div className="funnel-builder-quick-controls">
          {/* Conversion Window Duration */}
          <div className="funnel-ctrl-group">
            <label htmlFor="funnel-window-select" className="funnel-ctrl-label">
              Window:
            </label>
            <select
              id="funnel-window-select"
              className="dash-select"
              value={windowSeconds}
              onChange={(e) => onWindowChange(Number(e.target.value))}
            >
              <option value={3600}>1 Hour</option>
              <option value={86400}>24 Hours (1 Day)</option>
              <option value={604800}>7 Days</option>
              <option value={2592000}>30 Days</option>
            </select>
          </div>

          {/* Counting Mode */}
          <div className="funnel-ctrl-group">
            <label htmlFor="funnel-mode-select" className="funnel-ctrl-label">
              Count by:
            </label>
            <select
              id="funnel-mode-select"
              className="dash-select"
              value={countingMode}
              onChange={(e) => onCountingModeChange(e.target.value as 'users' | 'sessions')}
            >
              <option value="users">Unique Users</option>
              <option value="sessions">Unique Sessions</option>
            </select>
          </div>

          {/* Breakdown By Dimension */}
          <div className="funnel-ctrl-group">
            <label htmlFor="funnel-breakdown-select" className="funnel-ctrl-label">
              Breakdown:
            </label>
            <select
              id="funnel-breakdown-select"
              className="dash-select"
              value={breakdownBy}
              onChange={(e) => onBreakdownChange(e.target.value)}
            >
              <option value="">None</option>
              <option value="utm_source">UTM Source</option>
              <option value="utm_medium">UTM Medium</option>
              <option value="utm_campaign">UTM Campaign</option>
              <option value="browser">Browser</option>
              <option value="os">Operating System</option>
              {catalog?.property_keys?.map((key) => (
                <option key={'pk-' + key} value={`prop:${key}`}>
                  Property: {key}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Step Reorder Cards (Drag-and-Drop + Keyboard Accessibility) */}
      <div className="funnel-step-cards">
        {steps.map((st, idx) => (
          <div
            key={'step-card-' + idx}
            className={`funnel-step-input-card${draggedIdx === idx ? ' is-dragging' : ''}`}
            draggable
            onDragStart={(e) => onDragStart(e, idx)}
            onDragOver={onDragOver}
            onDrop={(e) => onDrop(e, idx)}
          >
            <div className="funnel-step-input-card__drag-handle" title="Drag to reorder">
              ⋮⋮
            </div>

            <div className="funnel-step-input-card__badge">Step {idx + 1}</div>

            <div className="funnel-step-input-card__input-wrap">
              <input
                type="text"
                className="dash-input funnel-step-input"
                placeholder={`e.g. ${idx === 0 ? 'page_view' : idx === 1 ? 'add_to_cart' : 'purchase'}`}
                value={st}
                onChange={(e) => handleStepChange(idx, e.target.value)}
                list={`catalog-events-${idx}`}
              />
              <datalist id={`catalog-events-${idx}`}>
                {catalog?.event_names?.map((en) => (
                  <option key={en.name} value={en.name}>
                    {en.name} ({en.count.toLocaleString()} events)
                  </option>
                ))}
              </datalist>
            </div>

            {/* Reorder Buttons for Accessibility */}
            <div className="funnel-step-input-card__actions">
              <button
                type="button"
                className="dash-icon-btn"
                onClick={() => handleMoveStep(idx, idx - 1)}
                disabled={idx === 0}
                title="Move up"
                aria-label={`Move step ${idx + 1} up`}
              >
                ↑
              </button>
              <button
                type="button"
                className="dash-icon-btn"
                onClick={() => handleMoveStep(idx, idx + 1)}
                disabled={idx === steps.length - 1}
                title="Move down"
                aria-label={`Move step ${idx + 1} down`}
              >
                ↓
              </button>
              <button
                type="button"
                className="dash-icon-btn dash-icon-btn--danger"
                onClick={() => handleRemoveStep(idx)}
                disabled={steps.length <= 2}
                title="Remove step (minimum 2 steps required)"
                aria-label={`Remove step ${idx + 1}`}
              >
                ✕
              </button>
            </div>
          </div>
        ))}

        {/* Add Step Button */}
        {steps.length < 5 && (
          <button
            type="button"
            className="funnel-add-step-btn"
            onClick={handleAddStep}
            title="Add another funnel step (max 5)"
          >
            + Add Step {steps.length + 1}
          </button>
        )}
      </div>

      {/* Query Execution Trigger Bar */}
      <div className="funnel-builder-bottom">
        <span className="funnel-step-count-hint">
          {steps.length} of 5 steps configured · Loose sequential order evaluation
        </span>

        <button
          type="button"
          className="dash-btn dash-btn--primary"
          onClick={onRunQuery}
          disabled={loading || steps.some((s) => !s.trim())}
        >
          {loading ? 'Evaluating Funnel...' : '⚡ Calculate Funnel'}
        </button>
      </div>

      {/* Save Funnel Preset Modal */}
      {saveModalOpen && (
        <div className="modal-backdrop" onClick={() => setSaveModalOpen(false)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 className="modal-title">Save Funnel Preset</h3>
              <button
                type="button"
                className="dash-icon-btn"
                onClick={() => setSaveModalOpen(false)}
              >
                ✕
              </button>
            </div>
            <form onSubmit={handleSaveSubmit}>
              <div className="modal-body">
                <p style={{ margin: '0 0 1rem 0', fontSize: '0.875rem', color: 'var(--color-muted)' }}>
                  Save this {steps.length}-step funnel so your entire team can monitor conversion
                  drop-offs.
                </p>
                <label htmlFor="funnel-preset-name" className="funnel-ctrl-label">
                  Preset Name:
                </label>
                <input
                  id="funnel-preset-name"
                  type="text"
                  className="dash-input"
                  placeholder="e.g. Mobile Signup to Activation"
                  value={newFunnelName}
                  onChange={(e) => setNewFunnelName(e.target.value)}
                  autoFocus
                  required
                />
              </div>
              <div className="modal-footer">
                <button
                  type="button"
                  className="dash-btn"
                  onClick={() => setSaveModalOpen(false)}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="dash-btn dash-btn--primary"
                  disabled={saving || !newFunnelName.trim()}
                >
                  {saving ? 'Saving...' : 'Save Funnel'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
