export function escapeEmailText(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

// Validate URL schemes and structure to prevent argument injection,
// file/script execution, and malformed inputs.
export function isAllowedEmailURL(value: string): boolean {
  if (!value || typeof value !== 'string') return false
  if (/[\r\n]/.test(value) || value.includes('\0')) return false
  try {
    const parsed = new URL(value.trim())
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
      // Must have a non-empty hostname that does not start with a dash
      // to avoid CLI flag/argument injection in URL handlers (e.g. xdg-open/open).
      return !!parsed.hostname && !parsed.hostname.startsWith('-')
    }
    if (parsed.protocol === 'mailto:') {
      // Reject bare 'mailto:' without address or query, and reject targets
      // starting with a dash to prevent CLI flag/argument injection in URL handlers.
      const target = parsed.pathname
      if (target.startsWith('-')) return false
      return !!target || !!parsed.search
    }
    return false
  } catch {
    return false
  }
}

// Match raw text once: never run a second replacement over generated markup.
export function linkifyText(text: string): string {
  const pattern = /https?:\/\/[^\s<>"'{}|\\^`[\]]+|[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/g
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
