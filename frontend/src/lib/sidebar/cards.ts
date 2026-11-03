export interface NavigationEntry {
  id: string
  kind: 'inboxes' | 'system' | 'folder' | 'virtual'
  name: string
  accountId?: string
  folderId?: string
  path?: string
  folderType?: string
  totalCount?: number
  unreadCount?: number
  count?: number
}
export interface NavigationCard { id: string; name: string; hidden: boolean; collapsed: boolean; entries: string[] }
export interface NavigationCards { version: 1; cards: NavigationCard[]; quick: string[]; showApps: boolean }
const defaults = [ ['virtual', 'Virtual folders'], ['primary', 'Mail'], ['secondary', 'More'], ['pinned', 'Pinned folders'], ['favorites', 'Starred folders'] ]
export function normalizeCards(raw: string, entries: NavigationEntry[]): NavigationCards {
  let saved: Partial<NavigationCards> = {}
  try { saved = JSON.parse(raw) || {} } catch { /* First use or stale preference. */ }
  const valid = new Set(entries.map(e => e.id)), used = new Set<string>(), cardIds = new Set<string>()
  const cards: NavigationCard[] = []
  for (const value of Array.isArray(saved.cards) ? saved.cards : []) {
    if (!value || typeof value.id !== 'string' || cardIds.has(value.id)) continue
    cardIds.add(value.id)
    const ids = (Array.isArray(value.entries) ? value.entries : []).filter(id => {
      if (!valid.has(id) || used.has(id)) return false
      used.add(id); return true
    })
    cards.push({ id: value.id, name: typeof value.name === 'string' ? value.name.slice(0, 100) : value.id, hidden: value.hidden === true, collapsed: value.collapsed === true, entries: ids })
  }
  for (const [id, name] of defaults) if (!cardIds.has(id)) cards.push({ id, name, hidden: false, collapsed: false, entries: [] })
  for (const entry of entries) if (!used.has(entry.id)) {
    const group = entry.kind === 'virtual' || entry.kind === 'inboxes' ? 'virtual' : entry.kind === 'folder' ? 'pinned' : ['archive', 'spam', 'snoozed'].includes(entry.folderType || '') ? 'secondary' : 'primary'
    cards.find(c => c.id === group)!.entries.push(entry.id)
  }
  // A missing quick list means the layout predates favorites: seed the rail
  // with the primary destinations. An explicitly saved (even empty) list is
  // respected so a user who removed every favorite keeps the rail empty.
  const fallback = ['inboxes', 'starred', 'drafts', 'sent']
    .filter(id => valid.has(id))
    .concat(entries.filter(e => e.kind === 'folder').map(e => e.id))
  const quick = [...new Set(Array.isArray(saved.quick) ? saved.quick.filter(id => valid.has(id)) : fallback)].slice(0, 10)
  return { version: 1, cards, quick, showApps: saved.showApps === true }
}
export function moveEntry(state: NavigationCards, id: string, cardId: string, index: number): NavigationCards {
  if (!state.cards.some(c => c.id === cardId) || !state.cards.some(c => c.entries.includes(id))) return state
  const cards = state.cards.map(c => ({ ...c, entries: c.entries.filter(e => e !== id) }))
  const target = cards.find(c => c.id === cardId)!
  target.entries.splice(Math.max(0, Math.min(index, target.entries.length)), 0, id)
  return { ...state, cards }
}
export function moveCard(state: NavigationCards, id: string, delta: number): NavigationCards {
  const from = state.cards.findIndex(c => c.id === id), to = from + delta
  if (from < 0 || to < 0 || to >= state.cards.length) return state
  const cards = [...state.cards]; const [card] = cards.splice(from, 1); cards.splice(to, 0, card)
  return { ...state, cards }
}
export function quickEntries(state: NavigationCards, entries: NavigationEntry[]): NavigationEntry[] {
  const byId = new Map(entries.map(entry => [entry.id, entry]))
  // The explicit favorite order is shared by the rail and Cmd/Ctrl+1…0.
  return state.quick.flatMap(id => byId.has(id) ? [byId.get(id)!] : []).slice(0, 10)
}
export function moveQuickEntry(state: NavigationCards, id: string, targetId: string, after: boolean): NavigationCards {
  const quick = [...state.quick]
  const from = quick.indexOf(id)
  if (from < 0 || !quick.includes(targetId) || id === targetId) return state
  quick.splice(from, 1)
  const target = quick.indexOf(targetId)
  quick.splice(target + (after ? 1 : 0), 0, id)
  return { ...state, quick }
}
export function shortcutIndex(event: {code?: string; key: string; metaKey?: boolean; ctrlKey?: boolean; shiftKey?: boolean; altKey?: boolean}, mac: boolean): number {
  if (!(mac ? event.metaKey : event.ctrlKey) || event.shiftKey || event.altKey) return -1
  const digit = /^Digit[0-9]$/.test(event.code || '') ? event.code!.slice(-1) : event.key
  return /^[0-9]$/.test(digit) ? (Number(digit) + 9) % 10 : -1
}
