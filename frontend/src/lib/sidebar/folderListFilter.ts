// Folder-list filtering for the Kaikki kansiot panel: while the folder panel
// is open, the three quick-filter buttons (all / unread / later) prune the folder tree instead of the message list. Pure logic lives
// here so it is testable without mounting Svelte components.

export type FolderListFilter = '' | 'unread' | 'readLater' | 'replyLater'

// accountId -> folderId -> queued conversation count, per queue.
export type QueueFolderCounts = Record<string, Record<string, number>>
export interface QueueCountsByFilter {
  readLater?: QueueFolderCounts
  replyLater?: QueueFolderCounts
}

export interface FilterableFolder {
  id: string
  unreadCount?: number
}

export interface FilterableTree {
  folder?: FilterableFolder | null
  children?: FilterableTree[]
}

/** Whether a folder itself matches the active folder-list filter. */
export function folderMatches(f: FilterableFolder, accountId: string, filter: FolderListFilter, queueCounts: QueueCountsByFilter = {}): boolean {
  if (!filter) return true
  if (filter === 'unread') return (f.unreadCount ?? 0) > 0
  return (queueCounts.readLater?.[accountId]?.[f.id] ?? 0) > 0 || (queueCounts.replyLater?.[accountId]?.[f.id] ?? 0) > 0
}

export interface PrunedFolderTree<T extends FilterableTree> {
  trees: T[]
  /** Folder IDs kept only because a descendant matches; render as context. */
  contextIds: Set<string>
}

/**
 * Prune a folder tree to folders matching the filter. Ancestors of matching
 * folders stay visible (marked in contextIds) so nested matches keep their
 * place in the tree; non-matching branches drop out entirely. The input is
 * never modified. An empty filter returns the input unchanged.
 */
export function pruneFolderTree<T extends FilterableTree>(trees: T[], accountId: string, filter: FolderListFilter, queueCounts: QueueCountsByFilter = {}): PrunedFolderTree<T> {
  if (!filter) return { trees, contextIds: new Set() }
  const contextIds = new Set<string>()
  const walk = (nodes: FilterableTree[]): FilterableTree[] => {
    const kept: FilterableTree[] = []
    for (const node of nodes) {
      const children = node.children ? walk(node.children) : []
      const self = !!node.folder && folderMatches(node.folder, accountId, filter, queueCounts)
      if (self) {
        kept.push(children.length !== (node.children?.length ?? 0) ? { ...node, children } : node)
      } else if (children.length) {
        if (node.folder?.id) contextIds.add(node.folder.id)
        kept.push({ ...node, children })
      }
    }
    return kept
  }
  return { trees: walk(trees) as T[], contextIds }
}
