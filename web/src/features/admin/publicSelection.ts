import type { PublicPageConfig } from '../../api/contracts'

export interface PublicPageDraft {
  overrides: Partial<PublicPageConfig>
  saved: PublicPageConfig | null
}

// Edited values remain local until the saved snapshot is acknowledged.
export function acknowledgePublicDraft(draft: PublicPageDraft, config: PublicPageConfig): PublicPageDraft {
  const saved = draft.saved
  if (!saved || saved.enabled !== config.enabled || saved.title !== config.title || saved.description !== config.description ||
      saved.nodeIds.length !== config.nodeIds.length || !saved.nodeIds.every((id) => config.nodeIds.includes(id))) return draft
  return { overrides: {}, saved: null }
}

// Selection operates on the complete draft, so filtering never loses hidden choices.
export function selectNodeIds(current: ReadonlySet<string>, ids: string[], checked: boolean): Set<string> {
  const next = new Set(current)
  for (const id of ids) { if (checked) next.add(id); else next.delete(id) }
  return next
}
