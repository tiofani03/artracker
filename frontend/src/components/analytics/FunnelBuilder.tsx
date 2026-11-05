import { useMemo, useState, type FormEvent } from 'react'
import type { EventCatalog, SavedFunnel } from '../../api'
import CustomSelect, { type CustomSelectOption } from '../CustomSelect'
import { POPULAR_TEMPLATES, formatEventName } from './friendlyNames'

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
  const [isEditingSteps, setIsEditingSteps] = useState(false)
  const [saveModalOpen, setSaveModalOpen] = useState(false)
  const [newFunnelName, setNewFunnelName] = useState('')
  const [saving, setSaving] = useState(false)

  const handleStepChange = (idx: number, val: string) => {
    const updated = [...steps]
    updated[idx] = val
    onStepsChange(updated)
  }

  const handleAddStep = () => {
    if (steps.length >= 5) return
    onStepsChange([...steps, ''])
    setIsEditingSteps(true)
  }

  const handleRemoveStep = (idx: number) => {
    if (steps.length <= 1) return
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

  // Window options
  const windowOptions: CustomSelectOption<number>[] = [
    { value: 3600, label: 'Window: 1 Hour' },
    { value: 86400, label: 'Window: 24 Hours' },
    { value: 604800, label: 'Window: 7 Days' },
    { value: 2592000, label: 'Window: 30 Days' },
  ]

  // Counting Mode options
  const countingModeOptions: CustomSelectOption<'users' | 'sessions'>[] = [
    { value: 'users', label: 'Count: Unique Users' },
    { value: 'sessions', label: 'Count: Sessions' },
  ]

  // Breakdown options
  const breakdownOptions: CustomSelectOption<string>[] = [
    { value: '', label: 'Breakdown: None' },
    { value: 'utm_source', label: 'Breakdown: UTM Source' },
    { value: 'utm_medium', label: 'Breakdown: UTM Medium' },
    { value: 'utm_campaign', label: 'Breakdown: UTM Campaign' },
    { value: 'browser', label: 'Breakdown: Browser' },
    { value: 'os', label: 'Breakdown: OS' },
    ...(catalog?.property_keys?.map((key) => ({
      value: `prop:${key}`,
      label: `Property: ${key}`,
    })) || []),
  ]

  // Combined Preset & Template Options
  const presetOptions: CustomSelectOption<string>[] = [
    { value: '', label: 'Load Preset or Template...' },
    ...POPULAR_TEMPLATES.map((tpl) => ({
      value: `template:${tpl.id}`,
      label: `Template: ${tpl.name}`,
      badge: `${tpl.steps.length} steps`,
    })),
    ...savedFunnels.map((sf) => ({
      value: `saved:${sf.id}`,
      label: `Saved: ${sf.name}`,
      badge: `${sf.steps.length} steps`,
    })),
  ]

  // Catalog Event Options for Dropdown Selection
  const catalogEventOptions: CustomSelectOption<string>[] = useMemo(() => {
    const list: CustomSelectOption<string>[] = [
      { value: '', label: 'Choose event from catalog...' },
    ]
    if (catalog?.event_names && catalog.event_names.length > 0) {
      for (const en of catalog.event_names) {
        list.push({
          value: en.name,
          label: formatEventName(en.name),
          meta: en.name,
          badge: `${en.count.toLocaleString()} occurrences`,
        })
      }
    } else {
      // Standard recommended events
      const defaults = ['landing_view', 'view_product', 'add_to_cart', 'checkout_start', 'purchase_completed']
      for (const d of defaults) {
        list.push({
          value: d,
          label: formatEventName(d),
          meta: d,
        })
      }
    }
    return list
  }, [catalog])

  const handlePresetSelect = (val: string) => {
    if (!val) return
    if (val.startsWith('template:')) {
      const tid = val.replace('template:', '')
      const tpl = POPULAR_TEMPLATES.find((t) => t.id === tid)
      if (tpl) onStepsChange(tpl.steps)
    } else if (val.startsWith('saved:')) {
      const sid = Number(val.replace('saved:', ''))
      const sf = savedFunnels.find((item) => item.id === sid)
      if (sf) onSelectSavedFunnel(sf)
    }
  }

  const isFunnelIncomplete = steps.filter((s) => s.trim() !== '').length < 2

  return (
    <div className="calm-funnel-toolbar">
      {/* Tier 1: Horizontal Funnel Steps Sequence */}
      <div className="calm-step-flow">
        <div className="calm-step-flow__header">
          <span className="calm-step-flow__label">Funnel Steps ({steps.length} of 5)</span>
          <div className="calm-step-flow__actions">
            <button
              type="button"
              className="dash-btn dash-btn--sm"
              onClick={() => setIsEditingSteps((prev) => !prev)}
            >
              {isEditingSteps ? 'Done Editing' : 'Edit Steps'}
            </button>
            {steps.length < 5 && (
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                onClick={handleAddStep}
              >
                Add Step
              </button>
            )}
          </div>
        </div>

        {/* Notice banner if fewer than 2 steps */}
        {isFunnelIncomplete && (
          <div className="calm-step-notice">
            <span>
              A conversion funnel requires at least 2 steps (e.g. Step 1 to Step 2) to calculate drop-offs and conversion rates.
            </span>
            {steps.length < 5 && (
              <button
                type="button"
                className="dash-btn dash-btn--primary dash-btn--sm"
                onClick={handleAddStep}
              >
                Add Step {steps.length + 1}
              </button>
            )}
          </div>
        )}

        {/* Pipeline Sequence with Inline Add Step Pill */}
        <div className="calm-step-flow__track">
          {steps.map((st, idx) => (
            <div key={'step-pill-' + idx} className="calm-step-pill-wrapper">
              <button
                type="button"
                className="calm-step-pill"
                onClick={() => setIsEditingSteps(true)}
                title="Click to edit or reorder step"
              >
                <span className="calm-step-pill__num">{idx + 1}</span>
                <span className="calm-step-pill__name">
                  {st ? formatEventName(st) : 'Select Event'}
                </span>
              </button>
              {idx < steps.length - 1 && (
                <span className="calm-step-pill__connector">then</span>
              )}
            </div>
          ))}

          {/* Direct Inline Add Step Button */}
          {steps.length < 5 && (
            <div className="calm-step-pill-wrapper">
              <span className="calm-step-pill__connector">then</span>
              <button
                type="button"
                className="calm-step-add-btn"
                onClick={handleAddStep}
                title="Add next funnel step"
              >
                Add Step {steps.length + 1}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Tier 2: Step Editor Drawer (When Open) */}
      {isEditingSteps && (
        <div className="calm-step-editor">
          <div className="calm-step-editor__head">
            <strong>Configure Step Sequence ({steps.length} of 5 steps)</strong>
            <span className="calm-step-editor__sub">
              Pick an event from your project catalog using the dropdown, or type a custom event name.
            </span>
          </div>

          <div className="calm-step-editor__list">
            {steps.map((st, idx) => (
              <div key={'editor-row-' + idx} className="calm-step-editor__row">
                <span className="calm-step-editor__badge">Step {idx + 1}</span>

                <div className="calm-step-editor__input-wrap">
                  {/* Dropdown Event Picker */}
                  <div className="calm-step-editor__select-picker">
                    <CustomSelect
                      size="sm"
                      searchable
                      options={catalogEventOptions}
                      value={catalogEventOptions.some((o) => o.value === st) ? st : ''}
                      onChange={(val) => {
                        if (val) handleStepChange(idx, val)
                      }}
                      placeholder="Pick catalog event..."
                      ariaLabel={`Pick catalog event for step ${idx + 1}`}
                    />
                  </div>

                  {/* Direct Text Input for Custom Event */}
                  <input
                    type="text"
                    className="dash-input calm-step-editor__input"
                    placeholder="or type event code (e.g. purchase_completed)"
                    value={st}
                    onChange={(e) => handleStepChange(idx, e.target.value)}
                  />
                </div>

                <div className="calm-step-editor__buttons">
                  <button
                    type="button"
                    className="calm-text-btn"
                    onClick={() => handleMoveStep(idx, idx - 1)}
                    disabled={idx === 0}
                  >
                    Up
                  </button>
                  <button
                    type="button"
                    className="calm-text-btn"
                    onClick={() => handleMoveStep(idx, idx + 1)}
                    disabled={idx === steps.length - 1}
                  >
                    Down
                  </button>
                  <button
                    type="button"
                    className="calm-text-btn calm-text-btn--danger"
                    onClick={() => handleRemoveStep(idx)}
                    disabled={steps.length <= 1}
                  >
                    Remove
                  </button>
                </div>
              </div>
            ))}
          </div>

          <div className="calm-step-editor__footer">
            {steps.length < 5 && (
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                onClick={handleAddStep}
              >
                Add Step {steps.length + 1}
              </button>
            )}
            <button
              type="button"
              className="dash-btn dash-btn--primary dash-btn--sm"
              onClick={() => setIsEditingSteps(false)}
            >
              Done Editing
            </button>
          </div>
        </div>
      )}

      {/* Tier 3: Query Controls Bar */}
      <div className="calm-query-bar">
        <div className="calm-query-bar__group">
          <div className="calm-query-bar__select">
            <CustomSelect
              size="sm"
              options={presetOptions}
              value=""
              onChange={handlePresetSelect}
              placeholder="Preset or Template..."
              ariaLabel="Select funnel preset or template"
            />
          </div>

          <div className="calm-query-bar__select">
            <CustomSelect
              size="sm"
              options={windowOptions}
              value={windowSeconds}
              onChange={onWindowChange}
              ariaLabel="Conversion window"
            />
          </div>

          <div className="calm-query-bar__select">
            <CustomSelect
              size="sm"
              options={countingModeOptions}
              value={countingMode}
              onChange={onCountingModeChange}
              ariaLabel="Counting mode"
            />
          </div>

          <div className="calm-query-bar__select">
            <CustomSelect
              size="sm"
              searchable
              options={breakdownOptions}
              value={breakdownBy}
              onChange={onBreakdownChange}
              ariaLabel="Breakdown dimension"
            />
          </div>
        </div>

        <div className="calm-query-bar__actions">
          <button
            type="button"
            className="dash-btn dash-btn--sm"
            onClick={() => setSaveModalOpen(true)}
          >
            Save Preset
          </button>

          <button
            type="button"
            className="dash-btn dash-btn--primary dash-btn--sm"
            onClick={onRunQuery}
            disabled={loading || steps.filter((s) => s.trim() !== '').length < 2}
          >
            {loading ? 'Running...' : 'Run Query'}
          </button>
        </div>
      </div>

      {/* Save Funnel Preset Modal */}
      {saveModalOpen && (
        <div className="modal-backdrop" onClick={() => setSaveModalOpen(false)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 className="modal-title">Save Funnel Preset</h3>
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                onClick={() => setSaveModalOpen(false)}
              >
                Close
              </button>
            </div>
            <form onSubmit={handleSaveSubmit}>
              <div className="modal-body">
                <p style={{ margin: '0 0 1rem 0', fontSize: '0.875rem', color: 'var(--color-muted)' }}>
                  Save this {steps.length}-step funnel sequence for fast reuse across your workspace.
                </p>
                <input
                  type="text"
                  className="dash-input"
                  placeholder="e.g. Checkout Activation Funnel"
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
                  {saving ? 'Saving...' : 'Save Preset'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
