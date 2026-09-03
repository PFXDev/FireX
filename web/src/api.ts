// Typed wrapper over the FireX admin API. Every call is cookie-authenticated;
// business-level 401 responses verify the session before stale auth is cleared.

export const FIREX_UNAUTHORIZED_EVENT = 'firex:unauthorized'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
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
    const error = new ApiError(res.status, data?.error ?? `请求失败 (${res.status})`)
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
  name: string
  emoji: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  missing: boolean
  /** How many node groups hold it. Zero means it reaches nobody. */
  groupCount: number
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
  hidden: boolean
  members: EgressMember[]
}

export interface RoutingMatrix {
  policies: Policy[]
  egresses: Egress[]
  options: {
    ruleTypes: string[]
    noResolveTypes: Record<string, boolean>
    groupTypes: string[]
    builtins: string[]
    memberKinds: MemberKind[]
  }
}

export interface Plan {
  id: number
  name: string
  profileId: number
  profileName: string
  trafficBytes: number
  durationDays: number
  deviceLimit: number
  speedNote: string
  enabled: boolean
  sortOrder: number
  remark: string
  usableInbounds: number
  userCount: number
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
  remark: string
  lastSubAt: number
  lastSubUa: string
  inboundCount: number
  syncState: string
  syncErrors: string[]
  subUrl: string
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
