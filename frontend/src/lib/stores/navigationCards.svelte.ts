import { accountStore } from './accounts.svelte'
import { getUIState, saveUIState } from './uiState.svelte'
import { normalizeCards, moveEntry, moveCard, moveQuickEntry, quickEntries, type NavigationEntry, type NavigationCards } from '../sidebar/cards'
let entries = $state<NavigationEntry[]>([])
let state = $state<NavigationCards>(normalizeCards('', []))
function visible(entry: NavigationEntry) {
  return entry.kind !== 'folder' || !accountStore.folderCatalog[entry.accountId || '']?.some(f => f.id === entry.folderId && f.hidden)
}
function commit(next: NavigationCards) { state = next; saveUIState({ sidebarCards: JSON.stringify(next) }) }
export const navigationCards = {
  get state() { return state },
  get entries() { return entries.filter(visible) },
  get quick() { return quickEntries(state, entries.filter(visible)) },
  sync(next: NavigationEntry[]) { entries = next; state = normalizeCards(getUIState().sidebarCards, next) },
  update: commit,
  moveEntry(id: string, card: string, index: number) { commit(moveEntry(state, id, card, index)) },
  moveCard(id: string, delta: number) { commit(moveCard(state, id, delta)) },
  moveQuick(id: string, targetId: string, after: boolean) { commit(moveQuickEntry(state, id, targetId, after)) },
  toggleQuick(id: string) { commit({ ...state, quick: state.quick.includes(id) ? state.quick.filter(e => e !== id) : [...state.quick, id].slice(0, 10) }) },
  patchCard(id: string, patch: Partial<NavigationCards['cards'][number]>) { commit({ ...state, cards: state.cards.map(c => c.id === id ? { ...c, ...patch } : c) }) },
  navigate(entry: NavigationEntry) { window.dispatchEvent(new CustomEvent('postilaatikko:navigate', { detail: entry })) },
  reset() { commit(normalizeCards('', entries)) },
}
