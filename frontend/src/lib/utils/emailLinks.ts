export function escapeEmailText(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

export function isAllowedEmailURL(value: string): boolean {
  try { return ['http:', 'https:', 'mailto:'].includes(new URL(value).protocol) } catch { return false }
}

// Match raw text once: never run a second replacement over generated markup.
export function linkifyText(text: string): string {
  const pattern = /https?:\/\/[^\s<>"'{}|\\^`\[\]]+|[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/g
  let result = '', end = 0
  for (const match of text.matchAll(pattern)) {
    const value = match[0], start = match.index!
    result += escapeEmailText(text.slice(end, start))
    const href = value.startsWith('http') ? value : `mailto:${value}`
    result += `<a href="${escapeEmailText(href)}" rel="noopener noreferrer" class="text-primary hover:underline">${escapeEmailText(value)}</a>`
    end = start + value.length
  }
  return result + escapeEmailText(text.slice(end))
}
