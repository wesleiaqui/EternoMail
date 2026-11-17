// Session-only reader find state. Never stored with mail or preferences.
export const messageFind = $state({ open: false, query: '' })

/** Highlight literal matches without parsing mail as HTML or changing links. */
export function highlightMessageText(root: HTMLElement, query: string): void {
  for (const mark of root.querySelectorAll('mark[data-reader-find]')) mark.replaceWith(...mark.childNodes)
  root.normalize()
  if (!query || query.length > 1000) return
  const document = root.ownerDocument
  const walker = document.createTreeWalker(root, 4)
  const nodes: Text[] = []
  let node: Node | null
  while ((node = walker.nextNode())) {
    if (node.parentElement?.closest('script,style,input,textarea,[contenteditable="true"]')) continue
    nodes.push(node as Text)
  }
  const needle = query.toLocaleLowerCase()
  let count = 0
  for (const node of nodes) {
    const text = node.data
    const lower = text.toLocaleLowerCase()
    let start = 0, index = lower.indexOf(needle)
    if (index < 0) continue
    const fragment = document.createDocumentFragment()
    while (index >= 0 && count < 2000) {
      fragment.append(document.createTextNode(text.slice(start, index)))
      const mark = document.createElement('mark')
      mark.dataset.readerFind = ''
      mark.style.cssText = 'background:#ffe084;color:#181818'
      mark.textContent = text.slice(index, index + query.length)
      fragment.append(mark); count++
      start = index + query.length
      index = lower.indexOf(needle, start)
    }
    fragment.append(document.createTextNode(text.slice(start)))
    node.replaceWith(fragment)
    if (count >= 2000) break
  }
  root.querySelector('mark[data-reader-find]')?.scrollIntoView({block:'nearest'})
}
