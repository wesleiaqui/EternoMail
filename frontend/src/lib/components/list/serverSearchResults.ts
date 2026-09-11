export type ServerSearchResult = {
  threadId: string
  accountId?: string
  folderId?: string
  messageIds?: string[]
  _uid?: number
}

// IMAP UIDs are only unique inside a mailbox. Keep the account and folder in
// the identity so Unified Inbox results cannot invalidate each other.
export function serverSearchResultIdentity(result: ServerSearchResult): string {
  const mailbox = `${result.accountId ?? ''}:${result.folderId ?? ''}`
  if (result._uid !== undefined && result._uid !== null) return `${mailbox}:uid:${result._uid}`
  if (result.messageIds?.[0]) return `${mailbox}:message:${result.messageIds[0]}`
  return `${mailbox}:thread:${result.threadId}`
}

export function removeServerSearchResults(
  results: readonly ServerSearchResult[],
  messageIds: Iterable<string> = [],
  identities: Iterable<string> = [],
): { results: ServerSearchResult[]; removed: number } {
  const affectedMessageIds = new Set(messageIds)
  const affectedIdentities = new Set(identities)
  const remaining = results.filter(result =>
    !affectedIdentities.has(serverSearchResultIdentity(result)) &&
    !result.messageIds?.some(id => affectedMessageIds.has(id))
  )
  return { results: remaining, removed: results.length - remaining.length }
}

export function updateServerSearchCounts(visibleCount: number, totalCount: number, removed: number) {
  const count = Math.max(0, visibleCount - removed)
  return { count, totalCount: Math.max(count, totalCount - removed) }
}

export type SearchResultSnapshot<T extends ServerSearchResult = ServerSearchResult> = {
  result: T
  index: number
}

export function restoreSearchResults<T extends ServerSearchResult>(
  results: readonly T[],
  snapshots: readonly SearchResultSnapshot<T>[],
): { results: T[]; restored: number } {
  const restoredResults = [...results]
  let restored = 0
  for (const snapshot of [...snapshots].sort((a, b) => a.index - b.index)) {
    if (restoredResults.some(result => serverSearchResultIdentity(result) === serverSearchResultIdentity(snapshot.result))) continue
    restoredResults.splice(Math.min(snapshot.index, restoredResults.length), 0, snapshot.result)
    restored += 1
  }
  return { results: restoredResults, restored }
}

/**
 * Discards superseded IMAP SEARCH replies and remembers rows removed by a
 * completed local action for the current query. This prevents an older server
 * reply from restoring a row that no longer belongs to the search mailbox.
 */
export class ServerSearchRequestState {
  private generation = 0
  private query = ''
  private invalidatedIdentities = new Set<string>()

  begin(query: string): number {
    if (query !== this.query) {
      this.query = query
      this.invalidatedIdentities.clear()
    }
    this.generation += 1
    return this.generation
  }

  invalidate(identities: Iterable<string>) {
    for (const identity of identities) this.invalidatedIdentities.add(identity)
    this.generation += 1
  }

  restore(identities: Iterable<string>) {
    for (const identity of identities) this.invalidatedIdentities.delete(identity)
    this.generation += 1
  }

  cancel() {
    this.generation += 1
  }

  accepts(generation: number, query: string): boolean {
    return generation === this.generation && query === this.query
  }

  filter<T extends ServerSearchResult>(results: readonly T[]): { results: T[]; suppressed: number } {
    const filtered = results.filter(result => !this.invalidatedIdentities.has(serverSearchResultIdentity(result)))
    return { results: filtered, suppressed: results.length - filtered.length }
  }
}
