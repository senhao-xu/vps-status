import { useI18n } from '../i18n'

export type NodeView = 'cards' | 'list'

export function filterNodes<T extends { name: string; group: string }>(nodes: T[], group: string, search: string): T[] {
  const query = search.trim().toLocaleLowerCase()
  return nodes.filter((node) => (group === '' || node.group === group) &&
    (!query || `${node.name} ${node.group}`.toLocaleLowerCase().includes(query)))
}

export function NodeFilters({ groups, group, onGroup, search, onSearch, view, onView }: {
  groups: string[]; group: string; onGroup: (group: string) => void
  search: string; onSearch: (search: string) => void; view: NodeView; onView: (view: NodeView) => void
}) {
  const { t } = useI18n()
  return <div className="node-toolbar">
    <div className="group-tabs" role="group" aria-label={t('groups')}>
      <button type="button" aria-pressed={group === ''} onClick={() => onGroup('')}>{t('allGroups')}</button>
      {groups.map((item) => <button type="button" key={item} aria-pressed={group === item} onClick={() => onGroup(item)}>{item}</button>)}
    </div>
    <div className="node-tools"><input aria-label={t('searchNodes')} type="search" value={search} placeholder={t('searchPlaceholder')} onChange={(event) => onSearch(event.target.value)} />
      <div className="view-toggle" role="group" aria-label={t('nodes')}>
        <button type="button" aria-label={t('cardView')} title={t('cardView')} aria-pressed={view === 'cards'} onClick={() => onView('cards')}><span aria-hidden="true">▦</span></button>
        <button type="button" aria-label={t('listView')} title={t('listView')} aria-pressed={view === 'list'} onClick={() => onView('list')}><span aria-hidden="true">☷</span></button>
      </div>
    </div>
  </div>
}

export function FleetSummary({ total, online, groups }: { total: number; online: number; groups: number }) {
  const { t } = useI18n()
  return <dl className="fleet-summary"><div><dt>{t('totalNodes')}</dt><dd>{total}</dd></div><div><dt>{t('online')}</dt><dd>{online}</dd></div><div><dt>{t('offline')}</dt><dd>{total - online}</dd></div><div><dt>{t('groups')}</dt><dd>{groups}</dd></div></dl>
}
