export interface BubbleMessage { id: string; fromEmail: string }

// Muted accents stay distinguishable in dark and light themes while retaining
// the same sender color throughout a thread.
function senderAccent(index: number): string {
  // Golden-angle spacing avoids a fixed palette repeating in larger groups.
  const hue = (205 + index * 137.508) % 360
  return `hsl(${hue.toFixed(1)} 52% 64%)`
}

/** Normalize envelope and display-address forms for presentation matching only. */
export function senderAddress(value: string): string {
  const text = (value || '').trim()
  return (text.match(/<\s*([^<>\s]+@[^<>\s]+)\s*>/)?.[1] || text).replace(/^mailto:/i, '').normalize('NFC').toLowerCase()
}

/** Messages from configured identities stay right; every other participant is
 * left-aligned. Participant accents are assigned in first-seen order so group
 * threads remain easy to scan without treating any sender as the account owner.
 */
export function bubbleLayout(messages: BubbleMessage[], isSelf: (email: string) => boolean) {
  const participants = [...new Set(messages.map(message => senderAddress(message.fromEmail)).filter(Boolean))]
  const accents = new Map(participants.map((sender, index) => [sender, senderAccent(index)]))
  return new Map(messages.map((message, index) => {
    const sender = senderAddress(message.fromEmail)
    return [message.id, {
      side: !!sender && isSelf(sender) ? 'right' : 'left',
      accent: accents.get(sender) || senderAccent(0),
      showSender: index === 0 || senderAddress(messages[index - 1].fromEmail) !== sender,
    }] as const
  }))
}

/** Double-clicking a control in a row must not spawn another reader. */
export function isRowOpenTarget(target: EventTarget | null, row: EventTarget | null): boolean {
  return target instanceof Element && target.closest('button, a, input, [role="checkbox"], [data-row-action]') === null && row instanceof Element
}
