// Typed wrapper over the FireX admin API. Every call is cookie-authenticated;
// business-level 401 responses verify the session before stale auth is cleared.

export const FIREX_UNAUTHORIZED_EVENT = 'firex:unauthorized'

export class ApiError extends Error {
  status: number
  fields: Record<string, string>
  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.status = status
    this.fields = fields
  }
}

type UnauthorizedHandling = 'verify-session' | 'session-invalid' | 'ignore'

type RequestOptions = {
  unauthorized?: UnauthorizedHandling
}

let sessionVerification: Promise<void> | null = null

function emitUnauthorized(path: string) {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(FIREX_UNAUTHORIZED_EVENT, { detail: { path } }))
}

function verifySession(path: string) {
  if (typeof window === 'undefined' || sessionVerification) return

  sessionVerification = fetch('/api/auth/me', {
    credentials: 'include',
  })
    .then((res) => {
      if (res.status === 401) emitUnauthorized(path)
    })
    .catch(() => {
      // A network/server failure does not prove the session is invalid. The
      // next protected request will retry verification if it also returns 401.
    })
    .finally(() => {
      sessionVerification = null
    })
}

function handleUnauthorized(path: string, handling: UnauthorizedHandling) {
  if (handling === 'session-invalid') {
    emitUnauthorized(path)
  } else if (handling === 'verify-session') {
    verifySession(path)
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    credentials: 'include',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch (err) {
    if (res.ok) throw err
  }
  if (!res.ok) {
    const error = new ApiError(res.status, data?.error ?? `请求失败 (${res.status})`, data?.fields)
    if (res.status === 401) handleUnauthorized(path, options.unauthorized ?? 'verify-session')
    throw error
  }
  return data as T
}

export const api = {
  get: <T,>(path: string, options?: RequestOptions) => request<T>('GET', path, undefined, options),
  post: <T,>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('POST', path, body ?? {}, options),
  put: <T,>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('PUT', path, body ?? {}, options),
  del: <T,>(path: string, options?: RequestOptions) =>
    request<T>('DELETE', path, undefined, options),
}

export interface Panel {
  id: number
  name: string
  baseUrl: string
  skipTlsVerify: boolean
  enabled: boolean
  remark: string
  status: string
  lastError: string
  lastSeenAt: number
  xrayVersion: string
  inboundCount: number
  enabledInbounds: number
  /** Cap on all FireX users' traffic here per period, in bytes (0 = unlimited). */
  trafficLimit: number
  /** Day of the month the period starts over (0 = never on its own). */
  trafficResetDay: number
  upload: number
  download: number
  used: number
  trafficPeriodStart: number
  /** Over its cap: the panel's inbounds are out of every subscription. */
  overQuota: boolean
  nextTrafficReset: number
  userCount: number
}

/** One inbound on one panel. Classification lives on the node group instead. */
export interface Inbound {
  id: number
  panelId: number
  panelName: string
  remoteId: number
  inboundTag: string
  protocol: string
  port: number
  remoteRemark: string
  remoteEnabled: boolean
  listen: string
  network: string
  security: string
  disableFlow: boolean
  vision: boolean | null
  visionSupported: boolean
  visionReason: string
  /** Render auto-mode XHTTP for mihomo as stream-up with connection reuse. */
  xhttpTuning: boolean
  /** Server-side XHTTP mode; empty is auto or not XHTTP. */
  xhttpMode: string
  lastSeenAt: number
  name: string
  emoji: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  /** Endpoint clients dial when it differs from what the panel advertises. Empty / 0 keep the panel's. */
  publicAddress: string
  publicPort: number
  /** Address the panel stores for its share links. Empty when the panel derives it. */
  shareAddress: string
  missing: boolean
  /** How many node groups hold it. Zero means it reaches nobody. */
  groupCount: number
  /** Empty when FireX does not manage this inbound's panel configuration. */
  configState: ConfigState
}

export type ConfigState = '' | 'synced' | 'pending' | 'drifted' | 'missing'

export interface InboundDeployment {
  id: number
  inboundId: number
  templateId: number
  revision: number
  panelId: number
  remoteId: number
  mode: 'update' | 'create' | 'rebind'
  status: string
  error: string
  createdAt: number
  hasBackup?: boolean
}

export interface InboundDetail {
  parameters: Record<string, unknown> | null
  lastSeenAt: number
}

export interface NodeGroupTag {
  key: string
  value: string
}

/** A hand-picked bundle of inbounds, rendered as one proxy-group. */
export interface NodeGroup {
  id: number
  name: string
  emoji: string
  type: string
  testUrl: string
  interval: number
  tolerance: number
  multiplier: number
  sortOrder: number
  enabled: boolean
  remark: string
  tags: NodeGroupTag[]
  inboundIds: number[]
  usableInbounds: number
  profileCount: number
}

/** A tier's routing: which node groups its users reach, plus its egress column. */
export interface Profile {
  id: number
  name: string
  /** Takes every enabled node group instead of an explicit whitelist. */
  allGroups: boolean
  sortOrder: number
  enabled: boolean
  remark: string
  groupIds: number[]
  planCount: number
  usableInbounds: number
}

/**
 * How an egress member is expressed. Everything references a node group or
 * policy by its bare name, so renaming an emoji never orphans a reference.
 */
export type MemberKind = 'node-group' | 'policy' | 'builtin' | 'all-node-groups' | 'all-inbounds'

export interface EgressMember {
  kind: MemberKind
  ref: string
}

export interface PolicyRule {
  type: string
  value: string
  noResolve: boolean
  disabled: boolean
}

/** One matrix row: a reusable rule list plus its identity in the client. */
export interface Policy {
  id: number
  name: string
  icon: string
  /** Renders as MATCH; exactly one enabled policy carries it. */
  isFinal: boolean
  enabled: boolean
  remark: string
  rules: PolicyRule[]
}

/** One matrix cell. profileId 0 is the default column every profile falls back to. */
export interface Egress {
  policyIndex: number
  profileId: number
  type: string
  testUrl: string
  interval: number
  tolerance: number
  /** Omits the policy group and its rules entirely. */
  hidden: boolean
  /** Keeps routing active and hides the group in compatible clients. */
  clientHidden: boolean
  members: EgressMember[]
}

/**
 * A named rule list served to clients as a rule-provider. Policies reach it
 * through a RULE-SET rule whose value is the set's name.
 */
export interface RuleSet {
  /** 0 for a set created in this draft. */
  id: number
  name: string
  /** Classical rule lines without a target; blank lines and # comments allowed. */
  payload: string
  remark: string
}

export interface RoutingMatrix {
  policies: Policy[]
  egresses: Egress[]
  ruleSets: RuleSet[]
  /** Set by the schema migration until the matrix is saved once. */
  needsReview: boolean
  options: {
    ruleTypes: string[]
    noResolveTypes: Record<string, boolean>
    groupTypes: string[]
    builtins: string[]
    memberKinds: MemberKind[]
    /** Built-in layouts that can replace every policy at once. */
    presets: RoutingPreset[]
  }
}

export interface RoutingPreset {
  key: string
  name: string
  description: string
}

/** How often a plan's users start their traffic over. */
export type TrafficReset = 'none' | 'monthly' | 'calendar'

export interface Plan {
  id: number
  name: string
  profileId: number
  profileName: string
  trafficBytes: number
  durationDays: number
  deviceLimit: number
  trafficReset: TrafficReset
  speedNote: string
  enabled: boolean
  sortOrder: number
  remark: string
  usableInbounds: number
  userCount: number
  /** Per-user caps on single panels; panels not listed are only held to the plan total. */
  panelLimits: PlanPanelLimit[]
}

export interface PlanPanelLimit {
  planId: number
  panelId: number
  trafficBytes: number
}

export interface User {
  id: number
  username: string
  uuid: string
  subToken: string
  planId: number
  planName: string
  enabled: boolean
  depleted: boolean
  expiresAt: number
  trafficLimit: number
  upload: number
  download: number
  used: number
  /** The plan's reset policy, and when this user's usage next starts over (0 = never). */
  trafficReset: TrafficReset
  nextTrafficReset: number
  trafficPeriodStart: number
  remark: string
  lastSubAt: number
  lastSubUa: string
  inboundCount: number
  syncState: string
  syncErrors: string[]
  subUrl: string
  /** This period's usage on each panel the user holds a client on. */
  panels: UserPanelUsage[]
}

export interface UserPanelUsage {
  panelId: number
  panelName: string
  upload: number
  download: number
  /** The plan's cap on this panel (0 = none). */
  limit: number
  /** Why the panel is closed to the user, empty when it is not. */
  blocked: string
}

export interface Overview {
  counts: {
    panels: number
    inbounds: number
    enabledInbounds: number
    missingInbounds: number
    nodeGroups: number
    profiles: number
    plans: number
    users: number
    activeUsers: number
  }
  upload: number
  download: number
  failedSyncs: number
  panels: { id: number; name: string; status: string; lastError: string; lastSeenAt: number }[]
}

export interface SubscriptionPreview {
  subUrl: string
  entries: {
    name: string
    protocol: string
    panelId: number
    clashSupported: boolean
    link: string
  }[]
  warnings: string[]
  clash: string
  renderError: string
  userinfo: string
}

export interface VersionInfo {
  version: string
  commit: string
  buildTime: string
  updateEnabled: boolean
  updateChannel: string
  updateSource: string
  updateRepo: string
}

/** live states keep the token working: open → submitted ⇄ returned → merged | cancelled. */
export type AgentState = 'open' | 'submitted' | 'returned' | 'merged' | 'cancelled'

export interface SnapshotFieldChange {
  path: string
  before?: unknown
  after?: unknown
}

/** One row of a snapshot diff, keyed by name. */
export interface SnapshotChange {
  section: string
  key: string
  action: 'create' | 'update' | 'delete'
  fields?: SnapshotFieldChange[]
}

export interface SnapshotPreview {
  changes: SnapshotChange[]
  /** Panels discovered after the merge commits. */
  discover: string[]
  /** Panels deleted with every FireX client on them. */
  removedPanels: string[]
  reconcilePlans: number
}

export interface SnapshotOutcome {
  revision: string
  changes: SnapshotChange[]
  discover: { panel: string; error: string }[]
  reconciledPlans: number
  syncError: string
  remoteCleanupFailures: number
  /** Managed configurations the merge left pending; deploy them from the inbound config page. */
  pendingDeployments: { panel: string; remoteId: number; inboundId: number; revision: number }[]
}

/** One hand-off of the configuration to an outside agent, reviewed before it takes effect. */
export interface AgentSession {
  id: number
  task: string
  state: AgentState
  feedback: string
  summary: string
  revision: string
  submissions: number
  readAt: number
  lastSeenAt: number
  submittedAt: number
  expiresAt: number
  finishedAt: number
  createdAt: number
  live: boolean
  expired: boolean
  /** The configuration moved since the submission; merging returns it to the agent. */
  stale: boolean
  preview: SnapshotPreview | null
  result: SnapshotOutcome | null
}

/** A row of the agent history; GET the session for its diff. */
export interface AgentHistoryItem {
  id: number
  task: string
  state: AgentState
  live: boolean
  expired: boolean
  summary: string
  submissions: number
  changes: number
  admin: string
  createdAt: number
  finishedAt: number
}

/** One configuration change made through the admin API. */
export type AdminRole = 'super_admin' | 'operator'

export interface AdminAccount {
  id: number
  username: string
  role: AdminRole
  /** An operator's read-only access to panels, inbounds, node groups, routing and the template. */
  canViewConfig: boolean
  createdAt: number
  updatedAt: number
  /** Whether this is the signed-in admin, whose own account cannot be removed. */
  isSelf: boolean
}

export interface AuditLog {
  id: number
  actor: string
  action: string
  /** The object's name when the change was made. */
  target: string
  method: string
  path: string
  ip: string
  createdAt: number
}

export interface ServerConfig {
  listen: string
  dataDir: string
  dbPath: string
  subBaseUrl: string
  subUpdateInterval: string
  debug: boolean
  adminUser: string
  adminPassword: string
  syncInterval: string
  trafficInterval: string
  discoverInterval: string
  update: {
    enabled: boolean
    channel: string
    checkInterval: string
    source: string
    proxyBaseUrl: string
    repo: string
  }
}

export interface ServerConfigResponse {
  config: ServerConfig
  path: string
  revision: string
  restartRequired: boolean
  /** Saved settings that differ from the running process and only apply on the next start. */
  restartFields: string[]
  hasPendingAdminPassword: boolean
}

/** State machine: idle → checking → downloading → ready (dev) | applying → idle, or failed. */
export type UpdateState = 'idle' | 'checking' | 'downloading' | 'ready' | 'applying' | 'failed'

export interface UpdateStatus {
  state: UpdateState
  currentVersion: string
  latestVersion: string
  isPrerelease: boolean
  progress: number
  downloadProgress: number
  error: string
  lastCheck: string
  releaseNotes: string
}

export interface UpdateCheck {
  hasUpdate: boolean
  currentVersion: string
  latestVersion: string
  isPrerelease: boolean
  releaseNotes: string
  channel: string
}
