import type { Language } from './i18n'

function unavailable(language: Language): string { return language === 'zh' ? '不可用' : 'Unavailable' }

export function formatBytes(value: number | null | undefined, language: Language = 'en'): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return unavailable(language)
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let amount = Math.max(0, value)
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${amount.toFixed(index === 0 ? 0 : amount >= 10 ? 1 : 2)} ${units[index]}`
}

export function formatRate(value: number | null | undefined, language: Language = 'en'): string {
  return value === null || value === undefined ? unavailable(language) : `${formatBytes(value, language)}/s`
}

export function formatPercent(value: number | null | undefined, language: Language = 'en'): string {
  return value === null || value === undefined ? unavailable(language) : `${value.toFixed(1)}%`
}

export function formatDuration(seconds: number | null | undefined, language: Language = 'en'): string {
  if (seconds === null || seconds === undefined) return unavailable(language)
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (language === 'zh') return days > 0 ? `${days}天 ${hours}小时` : hours > 0 ? `${hours}小时 ${minutes}分钟` : `${minutes}分钟`
  return days > 0 ? `${days}d ${hours}h` : hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`
}

export function formatTime(value: string | null | undefined, language: Language = 'en'): string {
  if (!value) return language === 'zh' ? '从未' : 'Never'
  return new Intl.DateTimeFormat(language === 'zh' ? 'zh-CN' : 'en', {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short',
  }).format(new Date(value))
}

export function ratio(used: number | null | undefined, total: number | null | undefined): number | null {
  if (used === null || used === undefined || total === null || total === undefined || total <= 0) return null
  return (used / total) * 100
}
