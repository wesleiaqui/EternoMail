// Tooltips for the compact mail rail.
//
// The expanded sidebar already contains readable labels. In collapsed mode
// those labels are intentionally hidden, but they remain in the DOM, so we
// can reuse the exact translated text for the native tooltip instead of
// duplicating strings for every button.

function getCompactSidebarButton(target: EventTarget | null): HTMLButtonElement | null {
  if (!(target instanceof Element)) return null
  const button = target.closest<HTMLButtonElement>('button')
  if (!button) return null
  if (!button.closest('.spark-sidebar-rebuilt.spark-sidebar-collapsed')) return null
  return button
}

function inferTooltip(button: HTMLButtonElement): string | null {
  // Keep any explicit tooltip/ARIA label supplied by the Svelte component.
  const explicit = button.getAttribute('title')?.trim()
  if (explicit) return explicit

  const aria = button.getAttribute('aria-label')?.trim()
  if (aria) return aria

  // Most rail buttons still contain their translated text in a hidden span.
  const label = button.querySelector<HTMLElement>('[data-sidebar-label]')?.textContent?.trim()
  if (label) return label

  // The collapsed unified inbox intentionally renders as a dedicated icon-only
  // button, so it has no hidden label to read from.
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

// Event delegation means this also works for buttons created after account
// loading, folder expansion, hot reloads and sidebar mode changes.
document.addEventListener('pointerover', ensureCompactSidebarTooltip, { passive: true })
document.addEventListener('focusin', ensureCompactSidebarTooltip)
