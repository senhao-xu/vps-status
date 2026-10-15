export type Decoder<T> = (value: unknown) => T

type JSONObject = Record<string, unknown>

function object(value: unknown, label: string): JSONObject {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
  return value as JSONObject
}

function string(value: unknown, label: string): string {
  if (typeof value !== 'string') throw new Error(`${label} must be a string`)
  return value
}

function boolean(value: unknown, label: string): boolean {
  if (typeof value !== 'boolean') throw new Error(`${label} must be a boolean`)
  return value
}

function number(value: unknown, label: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${label} must be a finite number`)
  }
  return value
}

function optionalNumber(value: unknown, label: string): number | null {
  return value === null || value === undefined ? null : number(value, label)
}

function array<T>(value: unknown, label: string, decode: Decoder<T>): T[] {
  if (!Array.isArray(value)) throw new Error(`${label} must be an array`)
  return value.map((item, index) => decodeWithLabel(item, `${label}[${index}]`, decode))
}

function decodeWithLabel<T>(value: unknown, label: string, decode: Decoder<T>): T {
  try {
    return decode(value)
  } catch (error) {
    throw new Error(`${label}: ${error instanceof Error ? error.message : 'invalid value'}`)
  }
}

function stringArray(value: unknown, label: string): string[] {
  return array(value, label, (item) => string(item, label))
}

function iso(value: unknown, label: string): string {
  const result = string(value, label)
  if (Number.isNaN(Date.parse(result))) throw new Error(`${label} must be a timestamp`)
  return result
}

function allowedKeys(value: JSONObject, allowed: readonly string[], label: string): void {
  const allow = new Set(allowed)
  for (const key of Object.keys(value)) {
    if (!allow.has(key)) throw new Error(`${label} contains forbidden field ${key}`)
  }
}

export interface APIErrorBody {
  code: string
  message: string
}

export function decodeAPIError(value: unknown): APIErrorBody {
  const root = object(value, 'error response')
  allowedKeys(root, ['error'], 'error response')
  const error = object(root.error, 'error')
  allowedKeys(error, ['code', 'message'], 'error')
  return { code: string(error.code, 'error.code'), message: string(error.message, 'error.message') }
}

export interface AuthResponse {
  admin: { username: string; mfaEnabled: boolean }
  csrfToken: string
}

export function decodeAuthResponse(value: unknown): AuthResponse {
  const root = object(value, 'auth response')
  allowedKeys(root, ['admin', 'csrf_token'], 'auth response')
  const admin = object(root.admin, 'admin')
  allowedKeys(admin, ['username', 'mfa_enabled'], 'admin')
  return {
    admin: { username: string(admin.username, 'admin.username'), mfaEnabled: boolean(admin.mfa_enabled, 'admin.mfa_enabled') },
    csrfToken: string(root.csrf_token, 'csrf_token'),
  }
}

export interface SessionResponse {
  admin: { username: string; mfaEnabled: boolean }
  session: { id: string; createdAt: string; expiresAt: string }
}

export function decodeSessionResponse(value: unknown): SessionResponse {
  const root = object(value, 'session response')
  allowedKeys(root, ['admin', 'session'], 'session response')
  const admin = object(root.admin, 'admin')
  const session = object(root.session, 'session')
  allowedKeys(admin, ['username', 'mfa_enabled'], 'admin')
  allowedKeys(session, ['id', 'created_at', 'expires_at'], 'session')
  return {
    admin: { username: string(admin.username, 'admin.username'), mfaEnabled: boolean(admin.mfa_enabled, 'admin.mfa_enabled') },
    session: {
      id: string(session.id, 'session.id'),
      createdAt: iso(session.created_at, 'session.created_at'),
      expiresAt: iso(session.expires_at, 'session.expires_at'),
    },
  }
}

export function decodeSetupStatus(value: unknown): { configured: boolean } {
  const root = object(value, 'setup status')
  allowedKeys(root, ['configured'], 'setup status')
  return { configured: boolean(root.configured, 'configured') }
}

export interface Report {
  collectedAtUnixMilli: number
  cpuUsagePercent: number | null
  loadOne: number | null
  memoryUsedBytes: number | null
  memoryTotalBytes: number | null
  swapUsedBytes: number | null
  swapTotalBytes: number | null
  diskUsedBytes: number | null
  diskTotalBytes: number | null
  receiveBytesPerSecond: number | null
  transmitBytesPerSecond: number | null
  uptimeSeconds: number | null
  processCount: number | null
  tcpConnections: number | null
  udpConnections: number | null
  unavailable: MetricArea[]
}

const metricAreas = ['cpu', 'load', 'memory', 'disk', 'network', 'uptime', 'processes', 'connections'] as const
type MetricArea = typeof metricAreas[number]

function decodeUnavailable(value: unknown): MetricArea[] {
  if (value === undefined) return []
  const result = stringArray(value, 'report.unavailable')
  const seen = new Set<string>()
  for (const area of result) {
    if (!(metricAreas as readonly string[]).includes(area) || seen.has(area)) {
      throw new Error('report.unavailable contains an unknown or duplicate area')
    }
    seen.add(area)
  }
  return result as MetricArea[]
}

export function decodeReport(value: unknown): Report {
  const root = object(value, 'report')
  allowedKeys(root, ['collected_at_unix_milli', 'cpu', 'load', 'memory', 'disk', 'network', 'uptime_seconds', 'process_count', 'connections', 'unavailable'], 'report')
  const cpu = object(root.cpu, 'report.cpu')
  const load = object(root.load, 'report.load')
  const memory = object(root.memory, 'report.memory')
  const disk = object(root.disk, 'report.disk')
  const network = object(root.network, 'report.network')
  const connections = object(root.connections, 'report.connections')
  allowedKeys(cpu, ['usage_percent'], 'report.cpu')
  allowedKeys(load, ['one', 'five', 'fifteen'], 'report.load')
  allowedKeys(memory, ['total_bytes', 'used_bytes', 'available_bytes', 'swap_total_bytes', 'swap_used_bytes'], 'report.memory')
  allowedKeys(disk, ['total_bytes', 'used_bytes', 'free_bytes'], 'report.disk')
  allowedKeys(network, ['receive_bytes_per_second', 'transmit_bytes_per_second', 'receive_bytes_total', 'transmit_bytes_total'], 'report.network')
  allowedKeys(connections, ['tcp', 'udp'], 'report.connections')
  const unavailable = decodeUnavailable(root.unavailable)
  const missing = new Set<MetricArea>(unavailable)
  const cpuUsagePercent = number(cpu.usage_percent, 'report.cpu.usage_percent')
  const loadOne = number(load.one, 'report.load.one')
  number(load.five, 'report.load.five')
  number(load.fifteen, 'report.load.fifteen')
  const memoryTotalBytes = number(memory.total_bytes, 'report.memory.total_bytes')
  const memoryUsedBytes = number(memory.used_bytes, 'report.memory.used_bytes')
  number(memory.available_bytes, 'report.memory.available_bytes')
  const swapTotalBytes = number(memory.swap_total_bytes, 'report.memory.swap_total_bytes')
  const swapUsedBytes = number(memory.swap_used_bytes, 'report.memory.swap_used_bytes')
  const diskTotalBytes = number(disk.total_bytes, 'report.disk.total_bytes')
  const diskUsedBytes = number(disk.used_bytes, 'report.disk.used_bytes')
  number(disk.free_bytes, 'report.disk.free_bytes')
  const receiveBytesPerSecond = number(network.receive_bytes_per_second, 'report.network.receive_bytes_per_second')
  const transmitBytesPerSecond = number(network.transmit_bytes_per_second, 'report.network.transmit_bytes_per_second')
  number(network.receive_bytes_total, 'report.network.receive_bytes_total')
  number(network.transmit_bytes_total, 'report.network.transmit_bytes_total')
  const uptimeSeconds = number(root.uptime_seconds, 'report.uptime_seconds')
  const processCount = number(root.process_count, 'report.process_count')
  const tcpConnections = number(connections.tcp, 'report.connections.tcp')
  const udpConnections = number(connections.udp, 'report.connections.udp')
  return {
    collectedAtUnixMilli: number(root.collected_at_unix_milli, 'report.collected_at_unix_milli'),
    cpuUsagePercent: missing.has('cpu') ? null : cpuUsagePercent,
    loadOne: missing.has('load') ? null : loadOne,
    memoryUsedBytes: missing.has('memory') ? null : memoryUsedBytes,
    memoryTotalBytes: missing.has('memory') ? null : memoryTotalBytes,
    swapUsedBytes: missing.has('memory') ? null : swapUsedBytes,
    swapTotalBytes: missing.has('memory') ? null : swapTotalBytes,
    diskUsedBytes: missing.has('disk') ? null : diskUsedBytes,
    diskTotalBytes: missing.has('disk') ? null : diskTotalBytes,
    receiveBytesPerSecond: missing.has('network') ? null : receiveBytesPerSecond,
    transmitBytesPerSecond: missing.has('network') ? null : transmitBytesPerSecond,
    uptimeSeconds: missing.has('uptime') ? null : uptimeSeconds,
    processCount: missing.has('processes') ? null : processCount,
    tcpConnections: missing.has('connections') ? null : tcpConnections,
    udpConnections: missing.has('connections') ? null : udpConnections,
    unavailable,
  }
}

export interface Node {
  id: string
  name: string
  group: string
  hidden: boolean
  homepageVisible: boolean
  hostname: string
  os: string
  kernel: string
  architecture: string
  virtualization: string
  connected: boolean
  online: boolean
  lastReportAt: string | null
  report: Report | null
}

export function decodeNode(value: unknown): Node {
  const root = object(value, 'node')
  allowedKeys(root, [
    'id', 'name', 'group', 'hidden', 'homepage_visible', 'hostname', 'os', 'kernel', 'architecture', 'virtualization',
    'created_at', 'updated_at', 'connected', 'online', 'last_report_at', 'report',
  ], 'node')
  iso(root.created_at, 'node.created_at')
  iso(root.updated_at, 'node.updated_at')
  return {
    id: string(root.id, 'node.id'),
    name: string(root.name, 'node.name'),
    group: string(root.group, 'node.group'),
    hidden: boolean(root.hidden, 'node.hidden'),
    homepageVisible: root.homepage_visible === undefined ? false : boolean(root.homepage_visible, 'node.homepage_visible'),
    hostname: string(root.hostname, 'node.hostname'),
    os: string(root.os, 'node.os'),
    kernel: string(root.kernel, 'node.kernel'),
    architecture: string(root.architecture, 'node.architecture'),
    virtualization: string(root.virtualization, 'node.virtualization'),
    connected: boolean(root.connected, 'node.connected'),
    online: boolean(root.online, 'node.online'),
    lastReportAt: root.last_report_at === undefined ? null : iso(root.last_report_at, 'node.last_report_at'),
    report: root.report === undefined ? null : decodeReport(root.report),
  }
}

export function decodeNodes(value: unknown): Node[] {
  const root = object(value, 'nodes response')
  allowedKeys(root, ['nodes'], 'nodes response')
  return array(root.nodes, 'nodes', decodeNode)
}

export interface LiveEvent {
  kind: 'connected' | 'report' | 'stale' | 'disconnected' | 'deleted'
  nodeId: string
  connected: boolean
  online: boolean
  receivedAt: string
  report: Report | null
}

export function decodeLiveEvent(value: unknown): LiveEvent {
  const root = object(value, 'live event')
  allowedKeys(root, ['kind', 'node_id', 'connected', 'online', 'received_at', 'report'], 'live event')
  const kind = string(root.kind, 'live.kind')
  if (!['connected', 'report', 'stale', 'disconnected', 'deleted'].includes(kind)) throw new Error('unknown live event kind')
  const report = root.report === undefined ? null : decodeReport(root.report)
  if (kind === 'report' && report === null) throw new Error('report event is missing its report')
  if (kind !== 'report' && report !== null) throw new Error('non-report event contains a report')
  return {
    kind: kind as LiveEvent['kind'],
    nodeId: string(root.node_id, 'live.node_id'),
    connected: boolean(root.connected, 'live.connected'),
    online: boolean(root.online, 'live.online'),
    receivedAt: iso(root.received_at, 'live.received_at'),
    report,
  }
}

export function reduceNodeEvent(nodes: Node[], event: LiveEvent): Node[] {
  if (event.kind === 'deleted') return nodes.filter((node) => node.id !== event.nodeId)
  return nodes.map((node) => {
    if (node.id !== event.nodeId) return node
    if (node.lastReportAt !== null && event.kind !== 'report' && Date.parse(event.receivedAt) < Date.parse(node.lastReportAt)) return node
    switch (event.kind) {
      case 'connected':
        return { ...node, connected: true, online: false, lastReportAt: null, report: null }
      case 'stale':
      case 'disconnected':
        return { ...node, connected: event.connected, online: false }
      case 'report':
        if (node.lastReportAt !== null && Date.parse(event.receivedAt) < Date.parse(node.lastReportAt)) return node
        return { ...node, connected: event.connected, online: event.online, lastReportAt: event.receivedAt, report: event.report }
      case 'deleted':
        return node
    }
  })
}

export interface HostPoint {
  bucketStart: string
  cpuAverage: number | null
  memoryUsedAverage: number | null
  memoryTotalLast: number | null
  diskUsedAverage: number | null
  diskTotalLast: number | null
  receiveRateAverage: number | null
  transmitRateAverage: number | null
}

export function decodeHostPoints(value: unknown): HostPoint[] {
  const root = object(value, 'host history')
  return array(root.points, 'points', (item) => {
    const point = object(item, 'host point')
    return {
      bucketStart: iso(point.bucket_start, 'bucket_start'),
      cpuAverage: optionalNumber(point.cpu_average, 'cpu_average'),
      memoryUsedAverage: optionalNumber(point.memory_used_average, 'memory_used_average'),
      memoryTotalLast: optionalNumber(point.memory_total_last, 'memory_total_last'),
      diskUsedAverage: optionalNumber(point.disk_used_average, 'disk_used_average'),
      diskTotalLast: optionalNumber(point.disk_total_last, 'disk_total_last'),
      receiveRateAverage: optionalNumber(point.receive_rate_average, 'receive_rate_average'),
      transmitRateAverage: optionalNumber(point.transmit_rate_average, 'transmit_rate_average'),
    }
  })
}

export interface TrafficTotal {
  period: string
  receiveBytes: number
  transmitBytes: number
  updatedAt: string
}

export function decodeTraffic(value: unknown): TrafficTotal[] {
  const root = object(value, 'traffic response')
  return array(root.traffic, 'traffic', (item) => {
    const row = object(item, 'traffic row')
    return {
      period: string(row.period, 'period'),
      receiveBytes: number(row.receive_bytes, 'receive_bytes'),
      transmitBytes: number(row.transmit_bytes, 'transmit_bytes'),
      updatedAt: iso(row.updated_at, 'updated_at'),
    }
  })
}

export interface ProbeLatest {
  nodeId: string
  receivedAt: string
  success: boolean
  latencyMillis: number
  error: string
}

export interface ProbeTask {
  id: string
  name: string
  host: string
  port: number
  intervalSeconds: number
  autoJoin: boolean
  updatedAt: string
  nodeIds: string[]
  latest: ProbeLatest[]
}

function decodeProbeLatest(value: unknown): ProbeLatest {
  const root = object(value, 'latest probe result')
  const result = object(root.result, 'latest probe result.result')
  allowedKeys(root, ['node_id', 'task_id', 'received_at', 'result'], 'latest probe result')
  allowedKeys(result, ['task_id', 'scheduled_at_unix_milli', 'success', 'latency_millis', 'error'], 'latest probe result.result')
  string(root.task_id, 'task_id')
  string(result.task_id, 'result.task_id')
  number(result.scheduled_at_unix_milli, 'result.scheduled_at_unix_milli')
  return {
    nodeId: string(root.node_id, 'node_id'),
    receivedAt: iso(root.received_at, 'received_at'),
    success: boolean(result.success, 'result.success'),
    latencyMillis: number(result.latency_millis, 'result.latency_millis'),
    error: result.error === undefined ? '' : string(result.error, 'result.error'),
  }
}

export function decodeProbeTask(value: unknown): ProbeTask {
  const root = object(value, 'probe task')
  allowedKeys(root, ['id', 'name', 'host', 'port', 'interval_seconds', 'auto_join', 'node_ids', 'created_at', 'updated_at', 'latest'], 'probe task')
  iso(root.created_at, 'probe.created_at')
  return {
    id: string(root.id, 'probe.id'),
    name: string(root.name, 'probe.name'),
    host: string(root.host, 'probe.host'),
    port: number(root.port, 'probe.port'),
    intervalSeconds: number(root.interval_seconds, 'probe.interval_seconds'),
    autoJoin: root.auto_join === undefined ? false : boolean(root.auto_join, 'probe.auto_join'),
    updatedAt: iso(root.updated_at, 'probe.updated_at'),
    nodeIds: stringArray(root.node_ids, 'probe.node_ids'),
    latest: array(root.latest, 'probe.latest', decodeProbeLatest),
  }
}

export function decodeProbes(value: unknown): ProbeTask[] {
  const root = object(value, 'probes response')
  allowedKeys(root, ['probes'], 'probes response')
  return array(root.probes, 'probes', decodeProbeTask)
}

export interface ProbePoint {
  bucketStart: string
  attempts: number
  failures: number
  successRate: number
  latencyAverage: number | null
}

export function decodeProbePoints(value: unknown): ProbePoint[] {
  const root = object(value, 'probe history')
  return array(root.points, 'points', (item) => {
    const point = object(item, 'probe point')
    return {
      bucketStart: iso(point.bucket_start, 'bucket_start'),
      attempts: number(point.attempts, 'attempts'),
      failures: number(point.failures, 'failures'),
      successRate: number(point.success_rate, 'success_rate'),
      latencyAverage: optionalNumber(point.latency_average, 'latency_average'),
    }
  })
}

export interface NotificationChannel {
  id: string
  name: string
  kind: 'webhook' | 'telegram'
  enabled: boolean
  configured: boolean
}

export function decodeChannels(value: unknown): NotificationChannel[] {
  const root = object(value, 'channels response')
  allowedKeys(root, ['channels'], 'channels response')
  return array(root.channels, 'channels', (item) => {
    const channel = object(item, 'channel')
    allowedKeys(channel, ['id', 'name', 'kind', 'enabled', 'configured', 'created_at', 'updated_at'], 'channel')
    iso(channel.created_at, 'channel.created_at')
    iso(channel.updated_at, 'channel.updated_at')
    const kind = string(channel.kind, 'channel.kind')
    if (kind !== 'webhook' && kind !== 'telegram') throw new Error('unknown channel kind')
    return {
      id: string(channel.id, 'channel.id'),
      name: string(channel.name, 'channel.name'),
      kind,
      enabled: boolean(channel.enabled, 'channel.enabled'),
      configured: boolean(channel.configured, 'channel.configured'),
    }
  })
}

export interface AlertRule {
  id: string
  name: string
  scopeKind: 'node' | 'group'
  scopeValue: string
  metric: 'offline' | 'cpu' | 'memory' | 'disk' | 'traffic_rate'
  threshold: number
  durationSeconds: number
  recoveryHysteresis: number
  enabled: boolean
  channelIds: string[]
}

export function decodeRules(value: unknown): AlertRule[] {
  const root = object(value, 'rules response')
  allowedKeys(root, ['rules'], 'rules response')
  return array(root.rules, 'rules', (item) => {
    const rule = object(item, 'rule')
    allowedKeys(rule, [
      'id', 'name', 'scope_kind', 'scope_value', 'metric', 'threshold', 'duration_seconds', 'recovery_hysteresis',
      'enabled', 'reminder_interval_seconds', 'reminder_max_count', 'channel_ids', 'created_at', 'updated_at',
    ], 'rule')
    number(rule.reminder_interval_seconds, 'rule.reminder_interval_seconds')
    number(rule.reminder_max_count, 'rule.reminder_max_count')
    iso(rule.created_at, 'rule.created_at')
    iso(rule.updated_at, 'rule.updated_at')
    const scopeKind = string(rule.scope_kind, 'rule.scope_kind')
    const metric = string(rule.metric, 'rule.metric')
    if (scopeKind !== 'node' && scopeKind !== 'group') throw new Error('unknown scope kind')
    if (!['offline', 'cpu', 'memory', 'disk', 'traffic_rate'].includes(metric)) throw new Error('unknown alert metric')
    return {
      id: string(rule.id, 'rule.id'),
      name: string(rule.name, 'rule.name'),
      scopeKind,
      scopeValue: string(rule.scope_value, 'rule.scope_value'),
      metric: metric as AlertRule['metric'],
      threshold: number(rule.threshold, 'rule.threshold'),
      durationSeconds: number(rule.duration_seconds, 'rule.duration_seconds'),
      recoveryHysteresis: number(rule.recovery_hysteresis, 'rule.recovery_hysteresis'),
      enabled: boolean(rule.enabled, 'rule.enabled'),
      channelIds: stringArray(rule.channel_ids, 'rule.channel_ids'),
    }
  })
}

export interface StorageStats {
  state: { available: boolean; writable: boolean; reason: string; queueDepth: number; queueCapacity: number; dropped: number }
  mainBytes: number | null
  walBytes: number | null
  shmBytes: number | null
  totalBytes: number | null
  freeDiskBytes: number | null
  oldestBucket: string | null
  pageCount: number | null
  freelistPages: number | null
}

export function decodeStorageStats(value: unknown): StorageStats {
  const root = object(value, 'storage status')
  const state = object(root.state, 'storage.state')
  allowedKeys(root, ['state', 'main_bytes', 'wal_bytes', 'shm_bytes', 'total_bytes', 'free_disk_bytes', 'oldest_bucket', 'page_count', 'freelist_pages'], 'storage status')
  allowedKeys(state, ['available', 'writable', 'reason', 'queue_depth', 'queue_capacity', 'dropped', 'maintenance', 'updated_at'], 'storage.state')
  if (state.maintenance !== undefined) string(state.maintenance, 'state.maintenance')
  iso(state.updated_at, 'state.updated_at')
  return {
    state: {
      available: boolean(state.available, 'state.available'),
      writable: boolean(state.writable, 'state.writable'),
      reason: state.reason === undefined ? '' : string(state.reason, 'state.reason'),
      queueDepth: number(state.queue_depth, 'state.queue_depth'),
      queueCapacity: number(state.queue_capacity, 'state.queue_capacity'),
      dropped: number(state.dropped, 'state.dropped'),
    },
    mainBytes: optionalNumber(root.main_bytes, 'main_bytes'),
    walBytes: optionalNumber(root.wal_bytes, 'wal_bytes'),
    shmBytes: optionalNumber(root.shm_bytes, 'shm_bytes'),
    totalBytes: optionalNumber(root.total_bytes, 'total_bytes'),
    freeDiskBytes: optionalNumber(root.free_disk_bytes, 'free_disk_bytes'),
    oldestBucket: root.oldest_bucket === undefined ? null : iso(root.oldest_bucket, 'oldest_bucket'),
    pageCount: optionalNumber(root.page_count, 'page_count'),
    freelistPages: optionalNumber(root.freelist_pages, 'freelist_pages'),
  }
}

export interface PublicPageConfig {
  enabled: boolean
  title: string
  description: string
  nodeIds: string[]
}

export function decodePublicPageConfig(value: unknown): PublicPageConfig {
  const root = object(value, 'public page configuration')
  allowedKeys(root, ['enabled', 'title', 'description', 'node_ids', 'updated_at'], 'public page configuration')
  iso(root.updated_at, 'updated_at')
  return {
    enabled: boolean(root.enabled, 'enabled'),
    title: string(root.title, 'title'),
    description: string(root.description, 'description'),
    nodeIds: stringArray(root.node_ids, 'node_ids'),
  }
}

export interface PublicNode {
  name: string
  group: string
  online: boolean
  uptimeSeconds: number | null
  cpuUsagePercent: number | null
  memoryUsedBytes: number | null
  memoryTotalBytes: number | null
  diskUsedBytes: number | null
  diskTotalBytes: number | null
  receiveBytesPerSecond: number | null
  transmitBytesPerSecond: number | null
  monthReceiveBytes: number | null
  monthTransmitBytes: number | null
}

export interface PublicStatus {
  page: { enabled: boolean; title: string; description: string }
  nodes: PublicNode[]
}

const publicNodeKeys = [
  'name', 'group', 'online', 'uptime_seconds', 'cpu_usage_percent', 'memory_used_bytes', 'memory_total_bytes',
  'disk_used_bytes', 'disk_total_bytes', 'receive_bytes_per_second', 'transmit_bytes_per_second',
  'month_receive_bytes', 'month_transmit_bytes',
] as const

export function decodePublicStatus(value: unknown): PublicStatus {
  const root = object(value, 'public status')
  allowedKeys(root, ['page', 'nodes'], 'public status')
  const page = object(root.page, 'public status.page')
  allowedKeys(page, ['enabled', 'title', 'description'], 'public status.page')
  return {
    page: {
      enabled: boolean(page.enabled, 'page.enabled'),
      title: string(page.title, 'page.title'),
      description: string(page.description, 'page.description'),
    },
    nodes: array(root.nodes, 'public nodes', (item) => {
      const node = object(item, 'public node')
      allowedKeys(node, publicNodeKeys, 'public node')
      return {
        name: string(node.name, 'node.name'),
        group: string(node.group, 'node.group'),
        online: boolean(node.online, 'node.online'),
        uptimeSeconds: optionalNumber(node.uptime_seconds, 'node.uptime_seconds'),
        cpuUsagePercent: optionalNumber(node.cpu_usage_percent, 'node.cpu_usage_percent'),
        memoryUsedBytes: optionalNumber(node.memory_used_bytes, 'node.memory_used_bytes'),
        memoryTotalBytes: optionalNumber(node.memory_total_bytes, 'node.memory_total_bytes'),
        diskUsedBytes: optionalNumber(node.disk_used_bytes, 'node.disk_used_bytes'),
        diskTotalBytes: optionalNumber(node.disk_total_bytes, 'node.disk_total_bytes'),
        receiveBytesPerSecond: optionalNumber(node.receive_bytes_per_second, 'node.receive_bytes_per_second'),
        transmitBytesPerSecond: optionalNumber(node.transmit_bytes_per_second, 'node.transmit_bytes_per_second'),
        monthReceiveBytes: optionalNumber(node.month_receive_bytes, 'node.month_receive_bytes'),
        monthTransmitBytes: optionalNumber(node.month_transmit_bytes, 'node.month_transmit_bytes'),
      }
    }),
  }
}

export function decodeTOTPSetup(value: unknown): { secret: string; uri: string } {
  const root = object(value, 'TOTP setup')
  allowedKeys(root, ['secret', 'uri'], 'TOTP setup')
  return { secret: string(root.secret, 'secret'), uri: string(root.uri, 'uri') }
}

export function decodeRecoveryCodes(value: unknown): string[] {
  const root = object(value, 'recovery codes')
  allowedKeys(root, ['recovery_codes'], 'recovery codes')
  return stringArray(root.recovery_codes, 'recovery_codes')
}

export interface Enrollment { id: string; enrollmentToken: string; installCommand: string; enrollCommand: string; expiresAt: string }

export function decodeEnrollment(value: unknown): Enrollment {
  const root = object(value, 'enrollment')
  allowedKeys(root, ['id', 'enrollment_token', 'expires_at', 'install_command', 'enroll_command'], 'enrollment')
  return {
    id: string(root.id, 'id'),
    enrollmentToken: string(root.enrollment_token, 'enrollment_token'),
    installCommand: string(root.install_command, 'install_command'),
    enrollCommand: string(root.enroll_command, 'enroll_command'),
    expiresAt: iso(root.expires_at, 'expires_at'),
  }
}

export type VisualTheme = 'emerald' | 'classic'
export const builtInThemes: readonly VisualTheme[] = ['emerald', 'classic']

export function decodeAppearance(value: unknown): { defaultTheme: VisualTheme } {
  const root = object(value, 'appearance')
  allowedKeys(root, ['default_theme'], 'appearance')
  if (root.default_theme !== 'emerald' && root.default_theme !== 'classic') throw new Error('unknown built-in theme')
  return { defaultTheme: root.default_theme }
}

export type EnrollmentStatus =
  | { id: string; nodeName: string; expiresAt: string; status: 'pending' | 'expired'; nodeId?: never }
  | { id: string; nodeName: string; expiresAt: string; status: 'registered' | 'online'; nodeId: string }

export function decodeEnrollmentStatus(value: unknown): EnrollmentStatus {
  const root = object(value, 'enrollment status')
  allowedKeys(root, ['id', 'node_name', 'expires_at', 'status', 'node_id'], 'enrollment status')
  const base = { id: string(root.id, 'id'), nodeName: string(root.node_name, 'node_name'), expiresAt: iso(root.expires_at, 'expires_at') }
  switch (root.status) {
    case 'pending': case 'expired':
      if (root.node_id !== undefined) throw new Error('unconsumed enrollment contains a node ID')
      return { ...base, status: root.status }
    case 'registered': case 'online': return { ...base, status: root.status, nodeId: string(root.node_id, 'node_id') }
    default: throw new Error('unknown enrollment status')
  }
}

export function decodeCredential(value: unknown): { credential: string } {
  const root = object(value, 'credential rotation')
  allowedKeys(root, ['node_id', 'credential'], 'credential rotation')
  string(root.node_id, 'node_id')
  return { credential: string(root.credential, 'credential') }
}


export interface InstallationConfig {
  controlUrl: string
  agentReleaseVersion: string
  effectiveAgentReleaseVersion: string
  agentReleaseBaseUrl: string
  releaseConfigured: boolean
}

export function decodeInstallationConfig(value: unknown): InstallationConfig {
  const root = object(value, 'installation configuration')
  allowedKeys(root, ['control_url', 'agent_release_version', 'effective_agent_release_version', 'agent_release_base_url', 'release_configured'], 'installation configuration')
  return {
    controlUrl: string(root.control_url, 'control_url'),
    agentReleaseVersion: string(root.agent_release_version, 'agent_release_version'),
    effectiveAgentReleaseVersion: string(root.effective_agent_release_version, 'effective_agent_release_version'),
    agentReleaseBaseUrl: string(root.agent_release_base_url, 'agent_release_base_url'),
    releaseConfigured: boolean(root.release_configured, 'release_configured'),
  }
}
