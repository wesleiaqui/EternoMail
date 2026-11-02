/** Refresh only the captured virtual view; a later navigation owns its own state. */
export async function refreshVirtualView(steps: {
  refreshCache: () => Promise<unknown>
  isCurrent: () => boolean
  reset: () => void
  reload: () => Promise<unknown>
}): Promise<void> {
  try {
    await steps.refreshCache()
  } catch (error) {
    if (!steps.isCurrent() || isExpectedSearchCancellation(error)) return
    throw error
  }
  if (!steps.isCurrent()) return
  steps.reset()
  await steps.reload()
}

/** Cancellation is expected when a newer search replaces a provider request. */
export function isExpectedSearchCancellation(error: unknown): boolean {
  if (error instanceof Error && error.name === 'AbortError') return true
  return /\bcontext cancel(?:ed|led)\b|\bsearch superseded\b/i.test(String(error))
}
