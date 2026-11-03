/** Insert text from the system clipboard into the currently focused editor. */
export function insertClipboardTextAtFocus(text: string): void {
  if (!text || !document.hasFocus()) return

  const active = document.activeElement
  if (!(active instanceof HTMLElement) || active instanceof HTMLButtonElement) return

  const input = active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement
  const editable = active.isContentEditable || active.closest('[contenteditable="true"]')
  if (!input && !editable) return
  if (input && (active as HTMLInputElement | HTMLTextAreaElement).disabled) return
  if (input && (active as HTMLInputElement | HTMLTextAreaElement).readOnly) return

  // insertText is permitted from a native menu callback, unlike execCommand
  //('paste'), which WebKit blocks because it is not a trusted paste gesture.
  // Keyboard ⌘V bypasses this path and remains a native paste for rich data.
  document.execCommand('insertText', false, text)
}
