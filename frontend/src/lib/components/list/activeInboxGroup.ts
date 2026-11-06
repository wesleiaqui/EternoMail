export interface PinnedInboxGroup<Group> {
  threadId: string
  group: Group
  index: number
}

export function pinnedGroupForSelection<Group>(
  threadId: string,
  selectedThreadId: string | null,
  pinned: PinnedInboxGroup<Group> | null,
): Group | null {
  if (!pinned || pinned.threadId !== threadId || selectedThreadId !== threadId) return null
  return pinned.group
}

export function retainPinnedConversationPosition<T extends { threadId: string }>(
  conversations: T[],
  threadId: string,
  index: number,
): T[] {
  if (index < 0) return conversations
  const currentIndex = conversations.findIndex(conversation => conversation.threadId === threadId)
  if (currentIndex < 0 || currentIndex === index) return conversations

  const result = [...conversations]
  const [selected] = result.splice(currentIndex, 1)
  result.splice(Math.min(index, result.length), 0, selected)
  return result
}
