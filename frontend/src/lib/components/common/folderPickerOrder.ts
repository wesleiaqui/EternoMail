/** Put the message's owning account first without disturbing other accounts. */
export function prioritizeSourceAccount<T extends { id: string }>(accounts: readonly T[], sourceAccountId: string): T[] {
  const sourceIndex = accounts.findIndex(item => item.id === sourceAccountId)
  if (sourceIndex <= 0) return [...accounts]
  return [accounts[sourceIndex], ...accounts.slice(0, sourceIndex), ...accounts.slice(sourceIndex + 1)]
}
