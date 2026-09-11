/** A row as it is actually reachable in the rendered message list. */
export type DisplayedConversation = {
  threadId: string
  unreadCount?: number
}

export type AutoSelectNextSnapshot = {
  originThreadId: string | null
  index: number
  preferUnread: boolean
}

export type DisplayGroup<T extends DisplayedConversation> = {
  id: string
  conversations: T[]
}

/** Flattens exactly the rows rendered by inbox cards, in display order. */
export function flattenVisibleInboxGroups<T extends DisplayedConversation, G extends DisplayGroup<T>>(
  groups: readonly G[],
  collapsedGroups: ReadonlySet<string>,
  chronological: boolean,
  visibleInGroup: (group: G) => readonly T[],
): T[] {
  return groups.flatMap(group =>
    chronological || !collapsedGroups.has(group.id) ? visibleInGroup(group) : []
  )
}

// Capture this before an action clears the viewer. In particular, leaving an
// unread conversation can asynchronously mark it read before the list reloads.
export function captureAutoSelectNext(
  conversations: readonly DisplayedConversation[],
  selectedThreadId: string | null,
  checkedThreadIds: ReadonlySet<string>,
): AutoSelectNextSnapshot {
  const index = checkedThreadIds.size > 0
    ? conversations.findIndex(conversation => checkedThreadIds.has(conversation.threadId))
    : conversations.findIndex(conversation => conversation.threadId === selectedThreadId)
  const origin = index >= 0 ? conversations[index] : undefined
  return {
    originThreadId: origin?.threadId ?? selectedThreadId,
    index,
    preferUnread: (origin?.unreadCount ?? 0) > 0,
  }
}

// The replacement row normally occupies the origin's former visual index.
// An unread origin advances only to a later visible unread row; it never wraps
// or falls back to a read row.
export function findAutoSelectNext(
  conversations: readonly DisplayedConversation[],
  snapshot: AutoSelectNextSnapshot,
): DisplayedConversation | null {
  if (snapshot.index < 0 || conversations.length === 0) return null

  let start = Math.min(snapshot.index, conversations.length - 1)
  if (conversations[start]?.threadId === snapshot.originThreadId) start += 1

  if (!snapshot.preferUnread) return conversations[start] ?? null

  for (let index = start; index < conversations.length; index += 1) {
    if ((conversations[index].unreadCount ?? 0) > 0) return conversations[index]
  }
  return null
}
