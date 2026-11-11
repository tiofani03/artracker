import { describe, expect, it } from 'vitest'
import type { AlertIncident, AlertRule } from '../../api'
import {
  buildRulePatch,
  draftFromRule,
  formatBaseline,
  formatCountdown,
  incidentHeadline,
  ruleDescription,
  ruleTitle,
  timeAgo,
  validateRulePatch,
} from './alertFormat'

function rule(over: Partial<AlertRule> = {}): AlertRule {
  return {
    kind: 'issue_spike',
    enabled: true,
    environment: 'production',
    spike_multiplier: 5,
    spike_min_count: 20,
    spike_window_minutes: 60,
    cooldown_minutes: 30,
    updated_at: '2026-11-11T00:00:00Z',
    ...over,
  }
}

function incident(over: Partial<AlertIncident> = {}): AlertIncident {
  return {
    id: 1,
    rule_kind: 'new_issue',
    issue_id: 7,
    status: 'firing',
    title: 'TypeError: x is undefined',
    environment: 'production',
    release_version: '1.0.0',
    session_id: '',
    spike: null,
    fired_at: '2026-11-11T00:00:00Z',
    resolved_at: null,
    notify_state: 'sent',
    ...over,
  }
}

describe('ruleTitle', () => {
  it('names each kind', () => {
    expect(ruleTitle('new_issue')).toBe('New issue')
    expect(ruleTitle('regression')).toBe('Regression')
    expect(ruleTitle('issue_spike')).toBe('Issue spike')
  })
})

describe('ruleDescription', () => {
  it('describes a spike rule', () => {
    expect(ruleDescription(rule())).toBe('≥ 20 events and 5× the usual rate within 60 min · production')
    expect(ruleDescription(rule({ environment: '' }))).toMatch(/· all environments$/)
  })
  it('describes new_issue and regression', () => {
    expect(ruleDescription(rule({ kind: 'new_issue' }))).toBe('The first time an error is seen · production')
    expect(ruleDescription(rule({ kind: 'regression' }))).toBe(
      'A resolved issue fails again and is reopened · production',
    )
    expect(ruleDescription(rule({ kind: 'new_issue', environment: '' }))).toBe(
      'The first time an error is seen · all environments',
    )
    expect(ruleDescription(rule({ kind: 'regression', environment: '' }))).toMatch(/· all environments$/)
  })
})

describe('validateRulePatch', () => {
  it('rejects bad spike values', () => {
    expect(validateRulePatch('issue_spike', { spike_window_minutes: 45 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { spike_multiplier: 1 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { spike_multiplier: 101 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { spike_min_count: 0 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { spike_min_count: 100001 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { spike_min_count: 2.5 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { cooldown_minutes: -1 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { cooldown_minutes: 1441 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { cooldown_minutes: 1.5 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { spike_min_count: Number.NaN })).not.toBeNull()
  })
  it('accepts valid values', () => {
    expect(validateRulePatch('issue_spike', { spike_min_count: 50 })).toBeNull()
    expect(validateRulePatch('issue_spike', { spike_multiplier: 1.5 })).toBeNull()
    expect(validateRulePatch('issue_spike', { spike_multiplier: 100 })).toBeNull()
    expect(validateRulePatch('issue_spike', { spike_window_minutes: 180 })).toBeNull()
    expect(validateRulePatch('issue_spike', { cooldown_minutes: 0 })).toBeNull()
    expect(validateRulePatch('issue_spike', { cooldown_minutes: 1440 })).toBeNull()
    expect(validateRulePatch('new_issue', { enabled: false })).toBeNull()
  })
  it('validates environment length', () => {
    expect(validateRulePatch('issue_spike', { environment: 'x'.repeat(65) })).not.toBeNull()
    expect(validateRulePatch('issue_spike', { environment: 'x'.repeat(64) })).toBeNull()
  })
  it('rejects spike fields on other kinds and empty patches', () => {
    expect(validateRulePatch('new_issue', { spike_multiplier: 5 })).not.toBeNull()
    expect(validateRulePatch('regression', { spike_min_count: 20 })).not.toBeNull()
    expect(validateRulePatch('regression', { cooldown_minutes: 10 })).not.toBeNull()
    expect(validateRulePatch('issue_spike', {})).not.toBeNull()
  })
})

describe('incidentHeadline', () => {
  it('summarises a spike', () => {
    const i = incident({ rule_kind: 'issue_spike', spike: { count: 142, baseline: 6, window_minutes: 60 } })
    expect(incidentHeadline(i)).toBe('142 events in 60 min (usual 6)')
    const j = incident({ rule_kind: 'issue_spike', spike: { count: 142, baseline: 0.208, window_minutes: 60 } })
    expect(incidentHeadline(j)).toBe('142 events in 60 min (usual 0.2)')
  })
  it('uses the title otherwise', () => {
    expect(incidentHeadline(incident())).toBe('TypeError: x is undefined')
    expect(incidentHeadline(incident({ rule_kind: 'regression' }))).toBe('TypeError: x is undefined')
  })
})

describe('formatBaseline', () => {
  it('rounds to one decimal without trailing zeros', () => {
    expect(formatBaseline(6)).toBe('6')
    expect(formatBaseline(0.208)).toBe('0.2')
    expect(formatBaseline(2.55)).toBe('2.6')
  })
})

describe('timeAgo', () => {
  const now = new Date('2026-11-11T12:00:00Z')
  it('formats elapsed time', () => {
    expect(timeAgo('2026-11-11T11:59:40Z', now)).toBe('just now')
    expect(timeAgo('2026-11-11T11:55:00Z', now)).toBe('5 min ago')
    expect(timeAgo('2026-11-11T09:00:00Z', now)).toBe('3 h ago')
    expect(timeAgo('2026-11-09T12:00:00Z', now)).toBe('2 d ago')
  })
  it('treats future or invalid input as just now', () => {
    expect(timeAgo('2026-11-11T13:00:00Z', now)).toBe('just now')
    expect(timeAgo('nope', now)).toBe('just now')
  })
})

describe('formatCountdown', () => {
  it('prints mm:ss and clamps at zero', () => {
    expect(formatCountdown(15 * 60_000)).toBe('15:00')
    expect(formatCountdown(65_000)).toBe('01:05')
    expect(formatCountdown(999)).toBe('00:01')
    expect(formatCountdown(-5)).toBe('00:00')
  })
})

describe('buildRulePatch', () => {
  it('is empty for an untouched draft', () => {
    const r = rule()
    expect(buildRulePatch(r, draftFromRule(r))).toEqual({})
  })
  it('contains only changed fields', () => {
    const r = rule()
    const d = { ...draftFromRule(r), enabled: false, minCount: '50', environment: ' staging ' }
    expect(buildRulePatch(r, d)).toEqual({ enabled: false, spike_min_count: 50, environment: 'staging' })
  })
  it('turns a blank number into NaN so validation rejects it', () => {
    const r = rule()
    const patch = buildRulePatch(r, { ...draftFromRule(r), multiplier: '' })
    expect(validateRulePatch('issue_spike', patch)).not.toBeNull()
  })
  it('ignores spike fields for other kinds', () => {
    const r = rule({ kind: 'new_issue' })
    expect(buildRulePatch(r, { ...draftFromRule(r), minCount: '99' })).toEqual({})
  })
})
