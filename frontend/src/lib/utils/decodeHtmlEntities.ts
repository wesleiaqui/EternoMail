/** Decode provider-escaped text for display without inserting it as markup. */
export function decodeHtmlEntities(value: string | undefined): string {
  if (!value || !value.includes('&') || typeof document === 'undefined') return value || ''

  // A textarea decodes character references into inert text. Rendering remains
  // Svelte text interpolation, so message content is never interpreted as HTML.
  const decoder = document.createElement('textarea')
  decoder.innerHTML = value
  return decoder.value
}
