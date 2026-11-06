# Palette's Journal

Critical UX and Accessibility learnings only.

## 2026-11-06 - Focus-Visible States on Hover-Revealed Attachment Action Controls
**Learning:** Secondary actions hidden by default with `opacity-0 group-hover:opacity-100` (such as attachment Open, Download, and Remove buttons) become invisible obstacles for keyboard navigation unless `focus-visible:opacity-100` is explicitly set along with focus ring classes. Additionally, icon-only buttons with `title` attributes must also provide explicit `aria-label` properties to guarantee consistent screen reader announcements.
**Action:** Whenever using `opacity-0 group-hover:opacity-100` on interactive controls, always pair it with `focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring` and explicit `aria-label` attributes.

## 2026-11-01 - Focus-Visible Opacity and Outline on Sidebar Card Action Buttons
**Learning:** In sidebar card rows (`NavigationCards.svelte`), action buttons rendered conditionally (such as edit and delete controls) must consistently specify `focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring` and meaningful `title` attributes. Without `focus-visible:opacity-100`, hover-revealed or subtle action controls can fail to render a visible focus state during keyboard tab navigation.
**Action:** When adding or editing secondary row action buttons in list and card components, always ensure `focus-visible:opacity-100` and the standard focus ring classes are included along with matching `aria-label` and `title` attributes.
