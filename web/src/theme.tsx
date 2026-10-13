import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'

import { apiRequest } from './api/client'
import { decodeAppearance, type VisualTheme } from './api/contracts'

export type Theme = 'dark' | 'light'

const themePreferenceKey = 'vps-status.theme'

interface StoredTheme { version: 1; value: Theme }

export function resolveInitialTheme(stored: string | null, prefersLight: boolean): Theme {
  if (stored !== null) {
    try {
      const parsed: unknown = JSON.parse(stored)
      if (typeof parsed === 'object' && parsed !== null && 'version' in parsed && 'value' in parsed &&
        parsed.version === 1 && (parsed.value === 'dark' || parsed.value === 'light')) return parsed.value
    } catch { /* Fall through to the system preference. */ }
  }
  return prefersLight ? 'light' : 'dark'
}

export const visualThemePreferenceKey = 'vps-status.visual-theme'

export function resolveVisualOverride(stored: string | null): VisualTheme | null {
  try {
    const parsed: unknown = stored === null ? null : JSON.parse(stored)
    if (typeof parsed === 'object' && parsed !== null && 'version' in parsed && 'value' in parsed &&
      parsed.version === 1 && (parsed.value === 'emerald' || parsed.value === 'classic')) return parsed.value
  } catch { /* Invalid preferences follow the site. */ }
  return null
}

function initialVisualOverride(): VisualTheme | null {
  try { return resolveVisualOverride(window.sessionStorage.getItem(visualThemePreferenceKey)) } catch { return null }
}

export async function loadSiteTheme(signal: AbortSignal): Promise<VisualTheme> {
  try { return (await apiRequest('/api/v1/public/appearance', decodeAppearance, { signal })).defaultTheme } catch { return 'emerald' }
}

interface ThemeContextValue {
  theme: Theme; setTheme: (theme: Theme) => void
  visualTheme: VisualTheme; visualOverride: VisualTheme | null; setVisualOverride: (theme: VisualTheme | null) => void
  siteTheme: VisualTheme; setSiteTheme: (theme: VisualTheme) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

function initialTheme(): Theme {
  if (typeof window === 'undefined') return 'dark'
  let stored: string | null = null
  try { stored = window.localStorage.getItem(themePreferenceKey) } catch { /* Storage may be unavailable. */ }
  return resolveInitialTheme(stored, window.matchMedia?.('(prefers-color-scheme: light)').matches ?? false)
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(initialTheme)
  const [siteTheme, setSiteThemeState] = useState<VisualTheme>('emerald')
  const siteRevision = useRef(0)
  const setSiteTheme = useCallback((value: VisualTheme) => { siteRevision.current++; setSiteThemeState(value) }, [])
  const [visualOverride, setVisualOverride] = useState<VisualTheme | null>(initialVisualOverride)
  const visualTheme = visualOverride ?? siteTheme
  useEffect(() => {
    const controller = new AbortController()
    const revision = siteRevision.current
    void loadSiteTheme(controller.signal).then((value) => { if (!controller.signal.aborted && siteRevision.current === revision) setSiteThemeState(value) })
    return () => controller.abort()
  }, [])
  useEffect(() => {
    document.documentElement.dataset.visualTheme = visualTheme
    try {
      if (visualOverride === null) window.sessionStorage.removeItem(visualThemePreferenceKey)
      else window.sessionStorage.setItem(visualThemePreferenceKey, JSON.stringify({ version: 1, value: visualOverride }))
    } catch { /* The UI still works without persistence. */ }
  }, [visualOverride, visualTheme])
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    const preference: StoredTheme = { version: 1, value: theme }
    try { window.localStorage.setItem(themePreferenceKey, JSON.stringify(preference)) } catch { /* The UI still works without persistence. */ }
  }, [theme])
  const value = useMemo(() => ({ theme, setTheme, visualTheme, visualOverride, setVisualOverride, siteTheme, setSiteTheme }), [theme, visualTheme, visualOverride, siteTheme, setSiteTheme])
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext)
  if (context === null) throw new Error('useTheme must be used inside ThemeProvider')
  return context
}
