// Reactive view of the pinned folders (P-007).
//
// The pins live in the persisted UI state. This store mirrors them in a
// $state so any sidebar row can ask whether its folder is pinned and update
// the list without passing callbacks through the folder tree.

import { accountStore } from './accounts.svelte'
import { getPinnedFolders, setPinnedFolders } from './uiState.svelte'
import { isPinned, togglePin, unpinFolder, type PinnedFolder } from '../sidebar/pins'

let pins = $state<PinnedFolder[]>(getPinnedFolders())

// Writes the list to the UI state and updates the local mirror.
function commit(next: PinnedFolder[]): void {
  pins = next
  setPinnedFolders(next)
}

export const sidebarPins = {
  // Pins in pin order.
  get list(): PinnedFolder[] {
    return pins.filter(pin => !accountStore.folderCatalog[pin.accountId]?.some(item => item.id === pin.folderId && item.hidden))
  },

  get all(): PinnedFolder[] { return pins },

  // Whether the account's folder is pinned.
  has(accountId: string, folderId: string): boolean {
    return isPinned(pins, accountId, folderId)
  },

  // Pins the folder, or unpins it when it is already pinned.
  toggle(accountId: string, folder: { id: string; path: string; name: string }): void {
    commit(togglePin(pins, { accountId, folderId: folder.id, path: folder.path, name: folder.name }))
  },

  // Removes the account's folder pin.
  unpin(accountId: string, folderId: string): void {
    commit(unpinFolder(pins, accountId, folderId))
  },

  // Re-reads the list after the UI state has been loaded from the backend.
  sync(): void {
    pins = getPinnedFolders()
  },
}
