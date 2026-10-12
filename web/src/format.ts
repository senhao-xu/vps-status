export function formatBytes(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return 'Unavailable'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let amount = Math.max(0, value)
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${amount.toFixed(index === 0 ? 0 : amount >= 10 ? 1 : 2)} ${units[index]}`
}

export function formatRate(value: number | null | undefined): string {
  return value === null || value === undefined ? 'Unavailable' : `${formatBytes(value)}/s`
}

export function formatPercent(value: number | null | undefined): string {
  return value === null || value === undefined ? 'Unavailable' : `${value.toFixed(1)}%`
}

export function formatDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) return 'Unavailable'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return days > 0 ? `${days}d ${hours}h` : hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`
}

export function formatTime(value: string | null | undefined): string {
  if (!value) return 'Never'
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short',
  }).format(new Date(value))
}

export function ratio(used: number | null | undefined, total: number | null | undefined): number | null {
  if (used === null || used === undefined || total === null || total === undefined || total <= 0) return null
  return (used / total) * 100
}
