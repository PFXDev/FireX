// Typed wrapper over the FireX admin API. Every call is cookie-authenticated;
// a 401 means the session lapsed, which App turns back into the login screen.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    credentials: 'include',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `请求失败 (${res.status})`)
  }
  return data as T
}

export const api = {
  get: <T,>(path: string) => request<T>('GET', path),
  post: <T,>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T,>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  del: <T,>(path: string) => request<T>('DELETE', path),
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
  nodeCount: number
  enabledNodes: number
}

export interface Node {
  id: number
  panelId: number
  panelName: string
  inboundId: number
  inboundTag: string
  protocol: string
  port: number
  remoteRemark: string
  remoteEnabled: boolean
  name: string
  region: string
  emoji: string
  tags: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  multiplier: number
  missing: boolean
  planCount: number
}

export interface Plan {
  id: number
  name: string
  trafficBytes: number
  durationDays: number
  deviceLimit: number
  speedNote: string
  enabled: boolean
  sortOrder: number
  remark: string
  nodeIds: number[]
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
  nodeCount: number
  syncState: string
  syncErrors: string[]
  subUrl: string
}

export interface Overview {
  counts: {
    panels: number
    nodes: number
    enabledNodes: number
    missingNodes: number
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
    region: string
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
