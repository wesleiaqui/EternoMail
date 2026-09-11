const mutationInFlightIds = new Set<string>()

export type MessageMutationResult<T> =
  | { started: false }
  | { started: true; value: T }

// Acquires every local message ID before invoking the backend. If any member
// of a batch is already moving, the whole batch is ignored; no partial call is
// made. Module scope shares the guard between rows, list actions, and viewer
// actions in the same WebView.
export async function runMessageMutation<T>(
  messageIds: Iterable<string>,
  mutation: () => Promise<T>,
): Promise<MessageMutationResult<T>> {
  const ids = [...new Set(messageIds)].filter(Boolean)
  if (ids.some(id => mutationInFlightIds.has(id))) {
    return { started: false }
  }

  for (const id of ids) mutationInFlightIds.add(id)
  try {
    return { started: true, value: await mutation() }
  } finally {
    for (const id of ids) mutationInFlightIds.delete(id)
  }
}
