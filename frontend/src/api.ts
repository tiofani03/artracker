export type Artwork = {
  id: number
  title: string
  artist: string
  year: number | null
  medium: string
  notes: string
  created_at: string
  updated_at: string
}

export type ArtworkInput = {
  title: string
  artist?: string
  year?: number | null
  medium?: string
  notes?: string
}

export type Role = 'owner' | 'admin' | 'member' | 'viewer'

export type OrgMembership = { id: number; name: string; slug: string; role: Role }

export type User = {
  id: number
  email: string
  name: string
  avatar_url: string
  orgs: OrgMembership[]
}

export const BASE: string = import.meta.env.VITE_API_BASE ?? '/api/v1'

export class UnauthorizedError extends Error {
  constructor() {
    super('unauthorized')
  }
}

// Access token lives in memory only; the refresh cookie (httpOnly) survives reloads.
let accessToken: string | null = null
export function setAccessToken(token: string | null) {
  accessToken = token
}
export function getAccessToken(): string | null {
  return accessToken
}

// Refresh tokens rotate and reuse revokes the session, so concurrent refreshes
// (StrictMode double effects, parallel 401s) must share one request.
let refreshing: Promise<void> | null = null
function refreshAccess(): Promise<void> {
  refreshing ??= (async () => {
    try {
      const res = await fetch(`${BASE}/auth/refresh`, { method: 'POST', credentials: 'include' })
      if (!res.ok) throw new UnauthorizedError()
      const body = (await res.json()) as { access_token: string }
      accessToken = body.access_token
    } catch (err) {
      accessToken = null
      throw err instanceof UnauthorizedError ? err : new UnauthorizedError()
    } finally {
      refreshing = null
    }
  })()
  return refreshing
}

async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : { 'X-Demo-Mode': '1' }),
      ...(init.headers ?? {}),
    },
  })

  if (res.status === 401 && retry && !path.startsWith('/auth/')) {
    await refreshAccess() // throws UnauthorizedError when the session is gone
    return request<T>(path, init, false)
  }

  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // non-JSON error body, keep the status text
    }
    throw new Error(message)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  list: () => request<{ items: Artwork[] }>('/artworks?limit=100'),
  create: (input: ArtworkInput) =>
    request<Artwork>('/artworks', { method: 'POST', body: JSON.stringify(input) }),
  remove: (id: number) => request<void>(`/artworks/${id}`, { method: 'DELETE' }),
}

export const authApi = {
  refresh: () => refreshAccess(),
  me: () => request<User>('/auth/me'),
  logout: async () => {
    await request<void>('/auth/logout', { method: 'POST' }, false)
    accessToken = null
  },
}

export type Organization = OrgMembership & { created_at: string }

export type Platform = 'web' | 'android' | 'ios'

export type Project = {
  id: number
  org_id: number
  name: string
  slug: string
  platform: Platform
  created_at: string
  /** Server-side switch: whether mobile SDKs may upload screenshot-mode replay (artracker-screen-v1). */
  replay_screenshot_enabled: boolean
}

export type KeyType = 'public_client' | 'secret_admin'

export type ApiKey = {
  id: number
  project_id: number
  key_type: KeyType
  prefix: string
  is_active: boolean
  created_at: string
  revoked_at: string | null
}

// secret is the plaintext key; the server returns it exactly once.
export type CreatedKey = { key: ApiKey; secret: string }

const post = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })

export const orgApi = {
  create: (name: string) => post<Organization>('/orgs', { name }),
  listProjects: (orgId: number) => request<{ items: Project[] }>(`/orgs/${orgId}/projects`),
  createProject: (orgId: number, input: { name: string; platform: Platform }) =>
    post<Project>(`/orgs/${orgId}/projects`, input),
}

export const keyApi = {
  list: (projectId: number) => request<{ items: ApiKey[] }>(`/projects/${projectId}/keys`),
  create: (projectId: number, keyType: KeyType) => post<CreatedKey>(`/projects/${projectId}/keys`, { key_type: keyType }),
  revoke: (projectId: number, keyId: number) => post<ApiKey>(`/projects/${projectId}/keys/${keyId}/revoke`),
  rotate: (projectId: number, keyId: number) => post<CreatedKey>(`/projects/${projectId}/keys/${keyId}/rotate`),
}

export type NavCounts = {
  frustrated_replays_24h: number
  open_issues: number
  firing_alerts: number
}

export const projectApi = {
  navCounts: (projectId: number) => request<NavCounts>(`/projects/${projectId}/nav-counts`),
  setReplaySettings: (projectId: number, body: { screenshot_enabled: boolean }) =>
    request<Project>(`/projects/${projectId}/replay-settings`, { method: 'PATCH', body: JSON.stringify(body) }),
}

export type AlertRuleKind = 'new_issue' | 'regression' | 'issue_spike'

export type AlertRule = {
  kind: AlertRuleKind
  enabled: boolean
  environment: string
  spike_multiplier: number
  spike_min_count: number
  spike_window_minutes: number
  cooldown_minutes: number
  updated_at: string
}

export type AlertRulePatch = {
  enabled?: boolean
  environment?: string
  spike_multiplier?: number
  spike_min_count?: number
  spike_window_minutes?: number
  cooldown_minutes?: number
}

export type AlertChannel = {
  id: number
  kind: string
  title: string
  chat_type: string
  enabled: boolean
  disabled_reason: string
  last_sent_at: string | null
  last_error: string
  created_at: string
  origin: 'linked' | 'manual'
  destination: string
  active: boolean
}

export type AlertBotMode = 'tenant' | 'shared'
export type AlertDetailLevel = 'minimal' | 'full'

export type AlertBot = {
  mode: AlertBotMode
  configured: boolean
  bot_username: string
  status: '' | 'active' | 'invalid'
  status_reason: string
  detail_level: AlertDetailLevel
  updated_at: string | null
}

export type AlertChannelsResponse = {
  mode: AlertBotMode
  bot: AlertBot
  telegram: { configured: boolean; bot_username: string }
  items: AlertChannel[]
}

export type AlertIncident = {
  id: number
  rule_kind: AlertRuleKind
  issue_id: number
  status: 'firing' | 'resolved'
  title: string
  environment: string
  release_version: string
  session_id: string
  spike: { count: number; baseline: number; window_minutes: number } | null
  fired_at: string
  resolved_at: string | null
  notify_state: string
}

export type AlertLinkCode = {
  code: string
  group_link: string
  private_link: string
  expires_at: string
}

export const alertApi = {
  rules: (projectId: number) => request<{ items: AlertRule[] }>(`/projects/${projectId}/alerts/rules`),
  patchRule: (projectId: number, kind: AlertRuleKind, body: AlertRulePatch) =>
    request<AlertRule>(`/projects/${projectId}/alerts/rules/${kind}`, { method: 'PATCH', body: JSON.stringify(body) }),
  channels: (projectId: number) => request<AlertChannelsResponse>(`/projects/${projectId}/alerts/channels`),
  createLinkCode: (projectId: number) => post<AlertLinkCode>(`/projects/${projectId}/alerts/channels/link-code`),
  setChannelEnabled: (projectId: number, channelId: number, enabled: boolean) =>
    request<AlertChannel>(`/projects/${projectId}/alerts/channels/${channelId}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    }),
  deleteChannel: (projectId: number, channelId: number) =>
    request<void>(`/projects/${projectId}/alerts/channels/${channelId}`, { method: 'DELETE' }),
  testChannel: (projectId: number, channelId: number) =>
    post<void>(`/projects/${projectId}/alerts/channels/${channelId}/test`),
  createChannel: (projectId: number, body: { chat: string; label: string }) =>
    post<AlertChannel & { warning?: string }>(`/projects/${projectId}/alerts/channels`, body),
  bot: (orgId: number) => request<AlertBot>(`/orgs/${orgId}/alerts/telegram-bot`),
  putBot: (orgId: number, body: { token: string; detail_level?: AlertDetailLevel }) =>
    request<AlertBot>(`/orgs/${orgId}/alerts/telegram-bot`, { method: 'PUT', body: JSON.stringify(body) }),
  patchBot: (orgId: number, body: { detail_level: AlertDetailLevel }) =>
    request<AlertBot>(`/orgs/${orgId}/alerts/telegram-bot`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteBot: (orgId: number) => request<void>(`/orgs/${orgId}/alerts/telegram-bot`, { method: 'DELETE' }),
  incidents: (projectId: number, status: 'firing' | 'resolved' | 'all' = 'all') =>
    request<{ items: AlertIncident[] }>(`/projects/${projectId}/alerts/incidents?status=${status}`),
}

export type BucketPoint = {
  label: string
  sessions: number
  errors: number
}

export type ProjectStats = {
  range: string
  sessions_count: number
  events_count: number
  errors_count: number
  error_free_pct: string
  error_note: string
  open_issues_count?: number
  frustrated_sessions_count?: number
  buckets: BucketPoint[]
}

export type TelemetryEvent = {
  id: string
  kind: 'event' | 'log' | 'session'
  name: string
  level: string
  message: string
  session_id: string
  user_id: string
  environment: string
  release_version: string
  event_timestamp: string
  attributes: Record<string, unknown>
}

export const telemetryApi = {
  getStats: (projectId: number, range = '24h') =>
    request<ProjectStats>(`/projects/${projectId}/stats?range=${range}`),
  listEvents: (projectId: number, kind = 'all', limit = 50) =>
    request<{ items: TelemetryEvent[] }>(`/projects/${projectId}/events?kind=${kind}&limit=${limit}`),
  seedMock: (projectId: number) =>
    post<{ status: string; seeded: number }>(`/projects/${projectId}/seed-mock`),
  sendTestEvent: (projectId: number) =>
    post<{ status: string; event_id: string }>(`/projects/${projectId}/test-event`),
}

export type IssueStatus = 'open' | 'resolved' | 'ignored'

export type AssignedUser = {
  id: number
  name: string
  email: string
}

export type Issue = {
  id: number
  project_id: number
  fingerprint: string
  title: string
  type: string
  status: IssueStatus
  first_seen: string
  last_seen: string
  count: number
  assigned_to: AssignedUser | null
  latest_message: string
}

export type Breadcrumb = {
  type?: string
  category?: string
  message?: string
  timestamp?: string
  [key: string]: unknown
}

export type DeviceInfo = {
  os?: string
  browser?: string
  screen?: string
  ip?: string
  location?: string
  user_agent?: string
  [key: string]: unknown
}

export type IssueEvent = {
  id: number
  session_id: string
  user_id: string
  message: string
  stack_trace: string
  stack_trace_resolved: string
  breadcrumbs: Breadcrumb[]
  device_info: DeviceInfo
  release_version: string
  created_at: string
}

export type IssueDetail = Issue & { events: IssueEvent[] }

export const issueApi = {
  list: (projectId: number, opts: { status?: string; q?: string; sort?: string } = {}) => {
    const params = new URLSearchParams()
    if (opts.status) params.set('status', opts.status)
    if (opts.q) params.set('q', opts.q)
    if (opts.sort) params.set('sort', opts.sort)
    return request<{ items: Issue[] }>(`/projects/${projectId}/issues?${params.toString()}`)
  },
  get: (projectId: number, issueId: number) =>
    request<IssueDetail>(`/projects/${projectId}/issues/${issueId}`),
  updateStatus: (projectId: number, issueId: number, status: IssueStatus) =>
    request<Issue>(`/projects/${projectId}/issues/${issueId}`, {
      method: 'PATCH',
      body: JSON.stringify({ status }),
    }),
  assign: (projectId: number, issueId: number) =>
    post<Issue>(`/projects/${projectId}/issues/${issueId}/assign`),
  uploadSourceMap: (projectId: number, version: string, content: unknown) =>
    post<{ status: string; file_name: string; sources: number }>(
      `/projects/${projectId}/releases/${version}/sourcemaps`,
      content,
    ),
}

export type FunnelStepResult = {
  step: number
  event_name: string
  count: number
  overall_pct: number
  step_conv_pct: number
  drop_off_count: number
  drop_off_pct: number
  median_seconds: number
}

export type FunnelBreakdown = {
  segment: string
  overall_conversion_pct: number
  step_counts: number[]
}

export type FunnelQueryResponse = {
  execution_ms: number
  window_seconds: number
  counting_mode: string
  overall_conversion_pct: number
  steps: FunnelStepResult[]
  breakdowns?: FunnelBreakdown[]
}

export type FunnelQueryRequest = {
  steps: string[]
  from?: string
  to?: string
  window_seconds?: number
  counting_mode?: 'users' | 'sessions'
  breakdown_by?: string
  property_filters?: Record<string, unknown>
}

export type CohortCell = {
  period: number
  retained_users: number
  percentage: number | null
  is_future: boolean
}

export type CohortRow = {
  cohort_label: string
  cohort_date: string
  cohort_size: number
  cells: CohortCell[]
}

export type CohortResponse = {
  granularity: string
  timezone: string
  start_event: string
  return_event: string
  cohorts: CohortRow[]
  weighted_averages: number[]
}

export type EventCatalog = {
  event_names: { name: string; count: number }[]
  utm_sources: string[]
  browsers: string[]
  operating_systems: string[]
  property_keys: string[]
}

export type SavedFunnel = {
  id: number
  project_id: number
  name: string
  steps: string[]
  window_seconds: number
  counting_mode: string
  breakdown_by: string
  created_by?: number
  created_at: string
  updated_at: string
}

export type SaveFunnelInput = {
  name: string
  steps: string[]
  window_seconds?: number
  counting_mode?: string
  breakdown_by?: string
}

export const analyticsApi = {
  catalog: (projectId: number) =>
    request<EventCatalog>(`/projects/${projectId}/analytics/catalog`),
  calculateFunnel: (projectId: number, req: FunnelQueryRequest) =>
    post<FunnelQueryResponse>(`/projects/${projectId}/analytics/funnel`, req),
  calculateCohort: (
    projectId: number,
    opts: {
      granularity?: string
      periods?: number
      start_event?: string
      return_event?: string
      tz?: string
    } = {},
  ) => {
    const params = new URLSearchParams()
    if (opts.granularity) params.set('granularity', opts.granularity)
    if (opts.periods) params.set('periods', String(opts.periods))
    if (opts.start_event) params.set('start_event', opts.start_event)
    if (opts.return_event) params.set('return_event', opts.return_event)
    if (opts.tz) params.set('tz', opts.tz)
    return request<CohortResponse>(`/projects/${projectId}/analytics/retention?${params.toString()}`)
  },
  listSavedFunnels: (projectId: number) =>
    request<{ items: SavedFunnel[] }>(`/projects/${projectId}/analytics/funnels`),
  createSavedFunnel: (projectId: number, input: SaveFunnelInput) =>
    post<SavedFunnel>(`/projects/${projectId}/analytics/funnels`, input),
  deleteSavedFunnel: (projectId: number, funnelId: number) =>
    request<void>(`/projects/${projectId}/analytics/funnels/${funnelId}`, { method: 'DELETE' }),
}

export type ReplaySessionItem = {
  id: string
  project_id: number
  user_id: string
  environment: string
  start_time: string
  end_time: string | null
  duration_ms: number
  chunk_count: number
  event_count: number
  total_size_bytes: number
  has_error: boolean
  has_rage_click: boolean
  has_dead_click: boolean
  storage_prefix: string
  created_at: string
  updated_at: string
}

export type ReplayChunkDetail = {
  chunk_seq: number
  object_key: string
  size_bytes: number
  event_count: number
  created_at: string
  url?: string
}

export type ReplaySessionListParams = {
  limit?: number
  offset?: number
  has_error?: boolean
  has_rage_click?: boolean
  has_dead_click?: boolean
  user_id?: string
  /** Case-insensitive prefix of the user id (the server treats % and _ literally). */
  user_prefix?: string
  environment?: string
  from?: string
  to?: string
}

export type ReplaySessionListResponse = {
  items: ReplaySessionItem[]
  total: number
  limit: number
  offset: number
}

export type ReplaySessionDetailResponse = {
  session: ReplaySessionItem
  chunks: ReplayChunkDetail[]
  timeline_events: TelemetryEvent[]
}

export type PerfNet = 'wifi' | 'cellular' | 'ethernet' | 'none'

export type PerfThermal = 'nominal' | 'fair' | 'serious' | 'critical'

/** One profiling reading; absent metrics were not measured (never 0). `t` is epoch ms. */
export type PerfSample = {
  t: number
  cpu?: number
  mem_mb?: number
  fps?: number
  jank?: number
  frozen?: number
  threads?: number
  battery?: number
  thermal?: PerfThermal
  /** Free device RAM / free disk space in MB. */
  ram_free_mb?: number
  storage_free_mb?: number
  net?: PerfNet
}

export type SessionPerfResponse = {
  interval_ms: number
  device: { ram_mb?: number; storage_mb?: number; os_version?: string; cores?: number; refresh_hz?: number }
  metrics: string[]
  samples: PerfSample[]
  downsampled: boolean
}

export const replayApi = {
  listSessions: (projectId: number, params: ReplaySessionListParams = {}) => {
    const qs = new URLSearchParams()
    if (params.limit !== undefined) qs.set('limit', String(params.limit))
    if (params.offset !== undefined) qs.set('offset', String(params.offset))
    if (params.has_error !== undefined) qs.set('has_error', String(params.has_error))
    if (params.has_rage_click !== undefined) qs.set('has_rage_click', String(params.has_rage_click))
    if (params.has_dead_click !== undefined) qs.set('has_dead_click', String(params.has_dead_click))
    if (params.user_id) qs.set('user_id', params.user_id)
    if (params.user_prefix) qs.set('user_prefix', params.user_prefix)
    if (params.environment) qs.set('environment', params.environment)
    if (params.from) qs.set('from', params.from)
    if (params.to) qs.set('to', params.to)
    const suffix = qs.toString() ? `?${qs.toString()}` : ''
    return request<ReplaySessionListResponse>(`/projects/${projectId}/sessions${suffix}`)
  },
  getSessionDetail: (projectId: number, sessionId: string) =>
    request<ReplaySessionDetailResponse>(
      `/projects/${projectId}/sessions/${encodeURIComponent(sessionId)}`,
    ),
  /** CPU / memory / FPS samples SDKs reported for the session (empty arrays when it has none). */
  getSessionPerf: (projectId: number, sessionId: string) =>
    request<SessionPerfResponse>(`/projects/${projectId}/sessions/${encodeURIComponent(sessionId)}/perf`),
  /** Freshly signed URLs for chunks `fromSeq..toSeq` of a session (used after earlier URLs expired). */
  listSessionChunks: (projectId: number, sessionId: string, fromSeq: number, toSeq: number) =>
    request<{ chunks: ReplayChunkDetail[] }>(
      `/projects/${projectId}/sessions/${encodeURIComponent(sessionId)}/chunks?from_seq=${fromSeq}&to_seq=${toSeq}`,
    ),
}

export type OrganizationMember = {
  user_id: number
  email: string
  name: string
  avatar_url: string
  role: Role
  joined_at: string
}

export type OrganizationInvitation = {
  id: number
  org_id: number
  email: string
  role: 'admin' | 'member' | 'viewer'
  invited_by: number
  invited_by_name?: string
  expires_at: string
  accepted_at?: string | null
  created_at: string
}

export type CreateInviteInput = {
  email: string
  role: 'admin' | 'member' | 'viewer'
}

export type CreateInviteResult = {
  id: number
  org_id: number
  email: string
  role: string
  invite_url: string
  token: string
  expires_at: string
}

export type InvitePreview = {
  org_id: number
  org_name: string
  org_slug: string
  email: string
  role: string
  invited_by_name: string
  expires_at: string
  is_expired: boolean
  is_accepted: boolean
}

export type AcceptInviteResult = {
  org_id: number
  org_name: string
  org_slug: string
  role: string
}

export const teamApi = {
  listMembers: (orgId: number) =>
    request<{ members: OrganizationMember[]; invitations: OrganizationInvitation[] }>(
      `/orgs/${orgId}/members`,
    ),
  createInvite: (orgId: number, input: CreateInviteInput) =>
    post<CreateInviteResult>(`/orgs/${orgId}/invites`, input),
  revokeInvite: (orgId: number, inviteId: number) =>
    request<void>(`/orgs/${orgId}/invites/${inviteId}`, { method: 'DELETE' }),
  updateMemberRole: (orgId: number, userId: number, role: 'admin' | 'member' | 'viewer') =>
    request<{ org_id: number; user_id: number; role: string }>(`/orgs/${orgId}/members/${userId}`, {
      method: 'PATCH',
      body: JSON.stringify({ role }),
    }),
  removeMember: (orgId: number, userId: number) =>
    request<void>(`/orgs/${orgId}/members/${userId}`, { method: 'DELETE' }),
  getInvitePreview: (token: string) =>
    request<InvitePreview>(`/invites/${encodeURIComponent(token)}`),
  acceptInvite: (token: string) =>
    post<AcceptInviteResult>(`/invites/${encodeURIComponent(token)}/accept`),
}

export type ReleaseArtifactItem = {
  id: number
  project_id: number
  version: string
  build_number: string
  artifact_type: 'dsym' | 'proguard_mapping' | 'sourcemap'
  file_name: string
  storage_path: string
  checksum_sha256: string
  cpu_arch?: string
  debug_uuid?: string
  size_bytes: number
  created_at: string
}

export const artifactsApi = {
  listArtifacts: (projectId: number, version: string) =>
    request<{ project_id: number; version: string; artifacts: ReleaseArtifactItem[] }>(
      `/projects/${projectId}/releases/${encodeURIComponent(version)}/artifacts`,
    ),
  uploadDSYM: (projectId: number, version: string, file: File | Blob, fileName?: string) => {
    const qs = fileName ? `?file=${encodeURIComponent(fileName)}` : ''
    return request<{ status: string; id: number; debug_uuid: string; cpu_arch: string }>(
      `/projects/${projectId}/releases/${encodeURIComponent(version)}/dsyms${qs}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: file,
      },
    )
  },
  uploadProguard: (projectId: number, version: string, file: File | Blob | string, fileName?: string) => {
    const qs = fileName ? `?file=${encodeURIComponent(fileName)}` : ''
    return request<{ status: string; id: number; file_name: string }>(
      `/projects/${projectId}/releases/${encodeURIComponent(version)}/proguard${qs}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: file,
      },
    )
  },
}
