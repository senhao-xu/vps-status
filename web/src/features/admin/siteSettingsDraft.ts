import type { PublicPageConfig } from '../../api/contracts'

export type SiteSettings = Pick<PublicPageConfig, 'enabled' | 'title' | 'description'>

export interface SiteSettingsDraft {
  overrides: Partial<SiteSettings>
  saved: SiteSettings | null
}

// Node visibility has a separate owner and never delays settings acknowledgement.
export function acknowledgeSiteDraft(draft: SiteSettingsDraft, config: PublicPageConfig): SiteSettingsDraft {
  const saved = draft.saved
  if (!saved || saved.enabled !== config.enabled || saved.title !== config.title || saved.description !== config.description) return draft
  return { overrides: {}, saved: null }
}
