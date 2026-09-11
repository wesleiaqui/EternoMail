/**
 * Tracks only messages that were unread when a conversation was first shown.
 * Refreshes never add IDs: a later external/manual unread choice must not be
 * overwritten when the user leaves the viewer.
 */
export class PendingReadOnLeave {
  private messageIds = new Set<string>()

  capture(messages: ReadonlyArray<{ id: string; isRead: boolean }>) {
    this.messageIds = new Set(messages.filter(message => !message.isRead).map(message => message.id))
  }

  cancel(messageIds: Iterable<string>) {
    for (const id of messageIds) this.messageIds.delete(id)
  }

  take(): string[] {
    const messageIds = [...this.messageIds]
    this.messageIds.clear()
    return messageIds
  }

  clear() {
    this.messageIds.clear()
  }
}
