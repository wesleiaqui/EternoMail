/** Keep the active row at its current index across a sorted-list refresh. */
export function retainReadSelection<T extends { threadId: string; unreadCount: number }>(previous: T[], next: T[], selected: string | null, unreadView: boolean): T[] {
  if (!selected) return next
  const index = previous.findIndex(item => item.threadId === selected)
  const row = previous[index]

  if (!row) return next

  const nextIndex = next.findIndex(item => item.threadId === selected)
  if (nextIndex >= 0) {
    if (nextIndex === index) return next
    const result = [...next]
    const [activeRow] = result.splice(nextIndex, 1)
    result.splice(Math.min(index, result.length), 0, activeRow)
    return result
  }

  if (!unreadView || row.unreadCount > 0) return next

  const result = [...next]
  result.splice(Math.min(index, result.length), 0, row)
  return result
}
