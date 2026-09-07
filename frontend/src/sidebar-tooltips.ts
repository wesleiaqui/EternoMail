// Tooltips for every action in the compact mail rail.
// Hidden translated labels remain in the DOM, so reuse them instead of
// maintaining a second set of tooltip strings.

function getCompactSidebarButton(target: EventTarget | null): HTMLButtonElement | null {
  if (!(target instanceof Element)) return null
  const button = target.closest<HTMLButtonElement>('button')
  if (!button) return null
  if (!button.closest('.spark-sidebar-rebuilt.spark-sidebar-collapsed')) return null
  return button
}

function inferTooltip(button: HTMLButtonElement): string | null {
  const explicit = button.getAttribute('title')?.trim()
  if (explicit) return explicit

  const aria = button.getAttribute('aria-label')?.trim()
  if (aria) return aria

  const label = button.querySelector<HTMLElement>('[data-sidebar-label]')?.textContent?.trim()
  if (label) return label

  if (button.matches('.sidebar-inbox-rail-button, [data-sidebar-nav-item="unified"]')) {
    return 'Caixa de entrada'
  }

  return null
}

function ensureCompactSidebarTooltip(event: Event): void {
  const button = getCompactSidebarButton(event.target)
  if (!button) return

  const tooltip = inferTooltip(button)
  if (tooltip && !button.hasAttribute('title')) {
    button.setAttribute('title', tooltip)
  }
}

document.addEventListener('pointerover', ensureCompactSidebarTooltip, { passive: true })
document.addEventListener('focusin', ensureCompactSidebarTooltip)
