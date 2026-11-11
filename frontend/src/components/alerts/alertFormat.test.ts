import { describe, expect, it } from 'vitest'
import type { AlertBot, AlertChannel, AlertIncident, AlertRule } from '../../api'
import {
  BOT_SETTINGS_PATH,
  CHAT_ID_HELP,
  botBannerText,
  canShowTest,
  countActiveChannels,
  showLinkFlow,
  botBadge,
  channelsBanner,
  isBotEndpointMissing,
  normalizeBot,
  normalizeChannelsResponse,
  normalizeToken,
  buildRulePatch,
  channelActivityLabel,
  countEnabledChannels,
  detailLevelLabel,
  detectLinked,
  draftFromRule,
  formatBaseline,
  formatCountdown,
  incidentHeadline,
  notifyLabel,
  ruleDescription,
  ruleTitle,
  timeAgo,
  validateBotToken,
  validateChat,
  validateLabel,
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

describe('notifyLabel', () => {
  it('labels the undelivered states', () => {
    expect(notifyLabel('pending')).toBe('sending…')
    expect(notifyLabel('skipped')).toBe('not sent (no channel, or no working bot for 24 h)')
    expect(notifyLabel('failed')).toBe('delivery failed')
  })
  it('hides sent and unknown states', () => {
    expect(notifyLabel('sent')).toBeNull()
    expect(notifyLabel('none')).toBeNull()
    expect(notifyLabel('')).toBeNull()
  })
})

function channel(id: number, enabled = true): AlertChannel {
  return {
    id,
    kind: 'telegram',
    title: `Chat ${id}`,
    chat_type: 'group',
    enabled,
    disabled_reason: '',
    last_sent_at: null,
    last_error: '',
    created_at: '2026-11-11T00:00:00Z',
    origin: 'linked',
    destination: '-1001',
    active: true,
  }
}

describe('detectLinked', () => {
  it('is true when a new channel appears', () => {
    expect(detectLinked([channel(1)], [channel(1), channel(2)])).toBe(true)
  })
  it('is false for the same list', () => {
    expect(detectLinked([channel(1), channel(2, false)], [channel(1), channel(2, false)])).toBe(false)
  })
  it('is true when a disabled channel is re-enabled by re-linking', () => {
    expect(detectLinked([channel(1, false)], [channel(1, true)])).toBe(true)
  })
  it('is false when a channel is disabled', () => {
    expect(detectLinked([channel(1, true)], [channel(1, false)])).toBe(false)
  })
  it('is true for a first channel', () => {
    expect(detectLinked([], [channel(1)])).toBe(true)
  })
  it('is false for a removal only', () => {
    expect(detectLinked([channel(1), channel(2)], [channel(1)])).toBe(false)
  })
})

describe('countEnabledChannels', () => {
  it('counts only enabled channels', () => {
    expect(countEnabledChannels([])).toBe(0)
    expect(countEnabledChannels([channel(1), channel(2, false), channel(3)])).toBe(2)
    expect(countEnabledChannels([channel(1, false)])).toBe(0)
  })
})

function bot(over: Partial<AlertBot> = {}): AlertBot {
  return {
    mode: 'tenant',
    configured: true,
    bot_username: 'acme_bot',
    status: 'active',
    status_reason: '',
    detail_level: 'minimal',
    updated_at: null,
    ...over,
  }
}

describe('validateChat', () => {
  it('accepts numeric ids and usernames', () => {
    expect(validateChat('-1001234567890')).toBeNull()
    expect(validateChat('123456')).toBeNull()
    expect(validateChat('@my_channel')).toBeNull()
  })
  it('rejects everything else, including surrounding whitespace', () => {
    for (const bad of ['', 'abc', '@a', '12', ' 123456 ', '00123', '-0123456', '+123456', '123456\n', '0']) expect(validateChat(bad)).not.toBeNull()
  })
})

describe('validateLabel', () => {
  // Choice: the label is not trimmed (a label with surrounding spaces is accepted as typed),
  // but an all-whitespace label counts as empty.
  it('enforces 1 to 80 characters', () => {
    expect(validateLabel('')).not.toBeNull()
    expect(validateLabel('a'.repeat(80))).toBeNull()
    expect(validateLabel('a'.repeat(81))).not.toBeNull()
  })
  it('counts code points, not UTF-16 units', () => {
    expect(validateLabel('😀'.repeat(80))).toBeNull()
    expect(validateLabel('😀'.repeat(81))).not.toBeNull()
  })
  it('rejects an all-whitespace label and accepts surrounding spaces', () => {
    expect(validateLabel('   ')).not.toBeNull()
    expect(validateLabel(' ops ')).toBeNull()
  })
  it('rejects control characters (labels are a single line)', () => {
    for (const bad of ['a\u0000b', 'a\nb', 'a\tb', '\u0085', 'ops\r', 'x\u007f']) {
      expect(validateLabel(bad)).toBe('The label cannot contain line breaks, tabs or control characters.')
    }
  })
  it('accepts normal text and emoji', () => {
    expect(validateLabel('Ops – prod alerts')).toBeNull()
    expect(validateLabel('Équipe 🚨 on-call')).toBeNull()
  })
})

describe('validateBotToken', () => {
  it('accepts a token-shaped value', () => {
    expect(validateBotToken('123456789:AAEhBP0av28kPoKy2VfQ3CwR4aFhEXAMPLE1')).toBeNull()
  })
  it('rejects malformed values without echoing them', () => {
    for (const bad of ['', 'abc', '123:short']) {
      const msg = validateBotToken(bad)
      expect(msg).not.toBeNull()
      if (bad !== '') expect(msg).not.toContain(bad)
    }
  })
})

describe('normalizeToken', () => {
  const tok = '123456789:AAEhBP0av28kPoKy2VfQ3CwR4aFhEXAMPLE1'
  it('removes surrounding spaces and a trailing newline', () => {
    expect(normalizeToken(`  ${tok}  `)).toBe(tok)
    expect(normalizeToken(`${tok}\n`)).toBe(tok)
    expect(normalizeToken(`\t${tok}\r\n`)).toBe(tok)
  })
  it('leaves inner characters untouched', () => {
    expect(normalizeToken(tok)).toBe(tok)
    expect(normalizeToken(' 123 456:AA b ')).toBe('123 456:AA b')
  })
  it('keeps validateBotToken strict: only the normalized value validates', () => {
    expect(validateBotToken(` ${tok}\n`)).not.toBeNull()
    expect(validateBotToken(normalizeToken(` ${tok}\n`))).toBeNull()
  })
})

describe('botBannerText', () => {
  it('asks an admin to connect a bot', () => {
    expect(botBannerText(bot({ configured: false, status: '' }), true)).toBe(
      'Connect a Telegram bot in Settings to receive alerts.',
    )
  })
  it('tells a member to ask an admin', () => {
    expect(botBannerText(bot({ configured: false, status: '' }), false)).toBe(
      'An organisation admin needs to connect a Telegram bot.',
    )
  })
  it('explains an invalid bot with its reason', () => {
    expect(botBannerText(bot({ status: 'invalid', status_reason: 'token revoked' }), true)).toContain('token revoked')
  })
  it('is always null in shared mode', () => {
    expect(botBannerText(bot({ mode: 'shared', configured: false, status: '' }), true)).toBeNull()
    expect(botBannerText(bot({ mode: 'shared', configured: false, status: '' }), false)).toBeNull()
    expect(botBannerText(bot({ mode: 'shared', status: 'invalid', status_reason: 'x' }), true)).toBeNull()
  })
  it('is null for an active bot', () => {
    expect(botBannerText(bot(), true)).toBeNull()
  })
})

describe('channelActivityLabel', () => {
  it('labels inactive channels by mode', () => {
    expect(channelActivityLabel({ ...channel(1), origin: 'linked', active: false }, 'tenant')).toBe(
      'belongs to the shared bot',
    )
    expect(channelActivityLabel({ ...channel(1), origin: 'manual', active: false }, 'shared')).toBe(
      'inactive in shared mode',
    )
  })
  it('is null when active', () => {
    expect(channelActivityLabel({ ...channel(1), origin: 'linked', active: true }, 'shared')).toBeNull()
    expect(channelActivityLabel({ ...channel(1), origin: 'manual', active: true }, 'tenant')).toBeNull()
  })
})

describe('detailLevelLabel', () => {
  it('describes each level', () => {
    expect(detailLevelLabel('minimal')).toBe('Minimal (no error text)')
    expect(detailLevelLabel('full')).toBe('Full (includes error text)')
  })
})

describe('canShowTest', () => {
  it('only offers Test for an active channel', () => {
    expect(canShowTest({ ...channel(1), active: true })).toBe(true)
    expect(canShowTest({ ...channel(1), active: false })).toBe(false)
  })
})

describe('countActiveChannels', () => {
  it('counts channels that are both active and enabled', () => {
    const items = [
      { ...channel(1), active: true, enabled: true },
      { ...channel(2), active: false, enabled: true },
      { ...channel(3), active: true, enabled: false },
    ]
    expect(countActiveChannels(items)).toBe(1)
  })
})

describe('showLinkFlow', () => {
  it('is only for shared mode', () => {
    expect(showLinkFlow('shared')).toBe(true)
    expect(showLinkFlow('tenant')).toBe(false)
  })
})

describe('botBadge', () => {
  it('maps status to a badge class and text', () => {
    expect(botBadge(bot())).toEqual({ className: 'badge--active', text: 'Active' })
    expect(botBadge(bot({ status: 'invalid', status_reason: 'token revoked' }))).toEqual({
      className: 'badge--revoked',
      text: 'Invalid: token revoked',
    })
    expect(botBadge(bot({ status: 'invalid', status_reason: '' }))).toEqual({
      className: 'badge--revoked',
      text: 'Invalid',
    })
  })
})

describe('legacy backend tolerance', () => {
  const legacy = {
    telegram: { configured: true, bot_username: 'old_bot' },
    items: [{ ...channel(1), enabled: true }],
  }
  const stripped = (() => {
    const c: Record<string, unknown> = { ...channel(1), enabled: true }
    delete c.origin
    delete c.destination
    delete c.active
    return c as unknown as AlertChannel
  })()
  const raw = { ...legacy, items: [stripped] }

  it('fills defaults for a payload without mode, bot, origin, active and destination', () => {
    const n = normalizeChannelsResponse(raw)
    expect(n.mode).toBe('shared')
    expect(n.bot).toBeUndefined()
    expect(channelsBanner(n, true)).toBeNull()
    expect(n.items[0].active).toBe(true)
    expect(n.items[0].origin).toBe('linked')
    expect(n.items[0].destination).toBe('')
    expect(showLinkFlow(n.mode)).toBe(true)
    expect(countActiveChannels(n.items)).toBe(1)
  })
  it('returns a full payload unchanged', () => {
    const full = {
      mode: 'tenant' as const,
      bot: bot({ mode: 'tenant' }),
      telegram: { configured: false, bot_username: '' },
      items: [{ ...channel(2), origin: 'manual' as const, destination: '@x_chan', active: false }],
    }
    expect(normalizeChannelsResponse(full)).toEqual(full)
  })
  it('counts legacy items without active by enabled', () => {
    expect(countActiveChannels([stripped, { ...stripped, id: 2, enabled: false }])).toBe(1)
    expect(canShowTest(stripped)).toBe(true)
    expect(channelActivityLabel(stripped, 'shared')).toBeNull()
  })
  it('uses the banner only when a bot is present', () => {
    const n = normalizeChannelsResponse({ ...raw, mode: 'tenant', bot: bot({ mode: 'tenant', configured: false, status: '' }) })
    expect(channelsBanner(n, true)).toBe('Connect a Telegram bot in Settings to receive alerts.')
  })
  it('normalizeBot rejects bodies without a mode and keeps valid ones', () => {
    expect(normalizeBot({})).toBeNull()
    expect(normalizeBot(null)).toBeNull()
    expect(normalizeBot(bot())).toEqual(bot())
  })
  it('detects the missing bot endpoint', () => {
    expect(isBotEndpointMissing(new Error('404 Not Found'))).toBe(true)
    expect(isBotEndpointMissing(new Error('500 Internal'))).toBe(false)
  })
  it("also matches Fiber's unmatched-route message from an older backend", () => {
    expect(isBotEndpointMissing(new Error('Cannot GET /api/v1/orgs/1/alerts/telegram-bot'))).toBe(true)
    expect(isBotEndpointMissing(new Error('404 Not Found'))).toBe(true)
    expect(isBotEndpointMissing(new Error('invalid bot token'))).toBe(false)
    expect(isBotEndpointMissing(new Error('tenant bots are disabled in this deployment'))).toBe(false)
    expect(isBotEndpointMissing('Cannot GET /x')).toBe(false)
  })
})

describe('CHAT_ID_HELP', () => {
  it('explains getUpdates in a private window without involving this app or third-party bots', () => {
    expect(CHAT_ID_HELP).toBe(
      'How to find the chat id: add your bot to the chat and send any message there. Then, in a private browser window, open https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates (replace <YOUR_TOKEN> with your bot\'s token) and look for "chat":{"id":...}. Groups and channels have ids that start with -100. Close the window afterwards and do not share that link. For a public channel you can enter @channelusername instead of a number.',
    )
    for (const phrase of ['private browser window', '-100', '@channelusername', '<YOUR_TOKEN>', 'do not share that link']) {
      expect(CHAT_ID_HELP).toContain(phrase)
    }
    for (const banned of ['@userinfobot', 'RawDataBot', '@getidsbot']) {
      expect(CHAT_ID_HELP).not.toContain(banned)
    }
  })
})

describe('BOT_SETTINGS_PATH', () => {
  it('opens Settings on the Team & Members tab, where the bot card is', () => {
    expect(BOT_SETTINGS_PATH).toBe('/dashboard/settings?tab=team')
  })
})
