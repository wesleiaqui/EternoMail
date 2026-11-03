// Keyboard shortcut registry — the descriptive half of keyboard handling.
//
// shortcuts.ts answers "does this KeyboardEvent match action X?". This file
// answers "what is action X called, which keys trigger it, and where does it
// apply?". The shortcuts overlay (components/shortcuts/ShortcutsDialog.svelte)
// and the tooltip / menu hint helpers (./format.ts) both read from here, so a
// label or key change shows up everywhere at once.
//
// Drift protection (frontend/tests/shortcutRegistry.test.mjs, run by
// `npm run check`):
//   - `predicates`: every displayed combo is turned into a synthetic event and
//     must satisfy one of the listed predicates (Mod combos are tried with
//     both Ctrl and Cmd); each listed predicate must accept one displayed
//     combo; every predicate exported from a KEY map must be described here.
//   - `inline`: bindings still written as switch cases inside App.svelte's
//     handleGlobalKeyDown or Composer.svelte's handleKeyDown name the key
//     literals they are bound by. Every listed literal must occur in that
//     handler, and every key literal in the handler must be listed by an
//     entry, so adding or removing a case without updating this file fails.
//   - `external`: bindings owned by a third-party library (TipTap formatting)
//     that the test cannot inspect. Keep these rare.
//
// Reuse by other surfaces: `id` is a stable action id, not a key name, and
// each entry carries the action's i18n label and (where it has one) the icon
// the GUI already uses for it. Menus, tooltips and a future contextual or
// selection toolbar should look actions up here (getShortcut / getShortcuts,
// plus withShortcut / ShortcutHint for the key label) instead of keeping
// their own label + key tables. Adding a toolbar button for an action with a
// shortcut is then one id reference.
//
// Extensions describe their own shortcuts in
// extensions/<name>/frontend/keyboard/registry.ts (default export of type
// ExtensionShortcutRegistry). They are discovered with import.meta.glob, the
// same file-drop convention extension i18n uses.
//
// Combo syntax: modifier tokens joined with '+', key last. Modifiers:
//   Mod   — Ctrl, or Cmd on macOS. Only use when the handler accepts
//           `ctrlKey || metaKey` (ctrlOrMeta).
//   Ctrl  — Control on every platform (e.g. Ctrl+Tab, which macOS reserves
//           as Cmd+Tab).
//   Meta  — Cmd only (macOS-specific bindings).
//   Alt, Shift.
// Keys: a single character ('r', '?', ',', '`'), or one of ArrowUp,
// ArrowDown, ArrowLeft, ArrowRight, Enter, Space, Escape, Tab, Backspace,
// Delete, ContextMenu, RightAlt, LeftAlt. Letters are written lowercase;
// Shift is spelled out (Shift+g).

import { KEY } from './shortcuts'

export type ShortcutPredicate = (e: KeyboardEvent) => boolean

/** Rail panes. 'mail' is the host; others are extension ids. */
export type ShortcutPane = string

export interface ShortcutInlineBinding {
  file: 'App.svelte' | 'Composer.svelte'
  /** Key literals exactly as they appear in the handler (case 'x': / === 'x'). */
  keys: string[]
}

export interface ShortcutDef {
  /** Stable id, used by hint helpers: e.g. 'mail.reply'. */
  id: string
  /** i18n key of the action label. */
  label: string
  /** Group id (see ShortcutGroupDef). */
  group: string
  /** Alternative combos; the first is the one shown in tooltips. */
  combos: string[]
  /** Iconify icon the GUI uses for this action (for toolbars / menus). */
  icon?: string
  /** Rail panes where the binding is active; omit for every pane. */
  panes?: ShortcutPane[]
  /** Only exists on macOS (e.g. Cmd+, for Preferences). */
  macOnly?: boolean
  predicates?: ShortcutPredicate[]
  inline?: ShortcutInlineBinding
  /** Name of the library that owns the binding, when neither of the above. */
  external?: string
}

export interface ShortcutGroupDef {
  id: string
  /** i18n key of the group heading. */
  label: string
}

export interface ExtensionShortcutRegistry {
  /** Extension id as used by the rail (getActiveExtension()). */
  extensionId: string
  groups: ShortcutGroupDef[]
  shortcuts: ShortcutDef[]
  /** Predicate map the extension registers, checked for coverage. */
  predicates: Record<string, ShortcutPredicate>
}

const MAIL: ShortcutPane[] = ['mail']

export const CORE_GROUPS: ShortcutGroupDef[] = [
  { id: 'general', label: 'shortcuts.group.general' },
  { id: 'navigation', label: 'shortcuts.group.navigation' },
  { id: 'sidebar', label: 'shortcuts.group.sidebar' },
  { id: 'messageList', label: 'shortcuts.group.messageList' },
  { id: 'messageActions', label: 'shortcuts.group.messageActions' },
  { id: 'viewer', label: 'shortcuts.group.viewer' },
  { id: 'composer', label: 'shortcuts.group.composer' },
  { id: 'lists', label: 'shortcuts.group.lists' },
]

const app = (...keys: string[]): ShortcutInlineBinding => ({ file: 'App.svelte', keys })
const composer = (...keys: string[]): ShortcutInlineBinding => ({ file: 'Composer.svelte', keys })

export const CORE_SHORTCUTS: ShortcutDef[] = [
  // General
  { id: 'app.commandPalette', icon: 'mdi:console-line', label: 'shortcuts.action.commandPalette', group: 'general', combos: ['Mod+p', 'Mod+Shift+p'], predicates: [KEY.COMMAND_PALETTE] },
  { id: 'app.shortcuts', icon: 'mdi:keyboard-outline', label: 'shortcuts.action.showShortcuts', group: 'general', combos: ['?'], predicates: [KEY.SHORTCUTS_HELP] },
  { id: 'app.preferences', label: 'shortcuts.action.preferences', group: 'general', combos: ['Meta+,'], macOnly: true, inline: app('Comma') },
  { id: 'app.compose', icon: 'mdi:pencil-plus-outline', label: 'shortcuts.action.compose', group: 'general', combos: ['c', 'Mod+n'], panes: MAIL, inline: app('c', 'n') },
  { id: 'app.search', icon: 'mdi:magnify', label: 'shortcuts.action.search', group: 'general', combos: ['/', 'Mod+s'], inline: app('/', 's') },
  { id: 'app.undo', icon: 'mdi:undo', label: 'shortcuts.action.undo', group: 'general', combos: ['z', 'Mod+z'], panes: MAIL, inline: app('z') },
  { id: 'app.syncAll', icon: 'mdi:refresh', label: 'shortcuts.action.syncAll', group: 'general', combos: ['Mod+Shift+a'], panes: MAIL, inline: app('a') },
  { id: 'app.syncFolder', icon: 'mdi:refresh', label: 'shortcuts.action.syncFolder', group: 'general', combos: ['Mod+Shift+s'], panes: MAIL, inline: app('s') },
  { id: 'app.nextRail', label: 'shortcuts.action.nextRail', group: 'general', combos: ['Ctrl+Tab'], inline: app('tab') },
  { id: 'app.prevRail', label: 'shortcuts.action.prevRail', group: 'general', combos: ['Ctrl+`'], inline: app('`') },
  { id: 'app.closeTab', label: 'shortcuts.action.closeTab', group: 'general', combos: ['Mod+w'], panes: MAIL, inline: app('w') },
  { id: 'app.quit', label: 'shortcuts.action.quit', group: 'general', combos: ['Mod+q'], inline: app('q') },

  // Pane and folder navigation (Alt chords work from any pane)
  { id: 'nav.paneNext', label: 'shortcuts.action.paneNext', group: 'navigation', combos: ['Alt+ArrowRight', 'Alt+l'], predicates: [KEY.PANE_FOCUS_NEXT] },
  { id: 'nav.panePrev', label: 'shortcuts.action.panePrev', group: 'navigation', combos: ['Alt+ArrowLeft', 'Alt+h'], predicates: [KEY.PANE_FOCUS_PREV] },
  { id: 'nav.folderNext', label: 'shortcuts.action.folderNext', group: 'navigation', combos: ['Alt+ArrowDown', 'Alt+j'], predicates: [KEY.SIDEBAR_NEXT] },
  { id: 'nav.folderPrev', label: 'shortcuts.action.folderPrev', group: 'navigation', combos: ['Alt+ArrowUp', 'Alt+k'], predicates: [KEY.SIDEBAR_PREV] },
  { id: 'nav.folderFirst', label: 'shortcuts.action.folderFirst', group: 'navigation', combos: ['Alt+g'], predicates: [KEY.SIDEBAR_FIRST] },
  { id: 'nav.folderLast', label: 'shortcuts.action.folderLast', group: 'navigation', combos: ['Alt+Shift+g'], predicates: [KEY.SIDEBAR_LAST] },
  { id: 'nav.toggleAccount', label: 'shortcuts.action.toggleAccount', group: 'navigation', combos: ['Alt+Enter'], panes: MAIL, inline: app('Enter') },
  { id: 'nav.folderMenu', label: 'shortcuts.action.folderMenu', group: 'navigation', combos: ['LeftAlt+RightAlt'], panes: MAIL, inline: app('AltLeft') },

  // Sidebar (folder list focused)
  { id: 'sidebar.next', label: 'shortcuts.action.sidebarNext', group: 'sidebar', combos: ['ArrowDown', 'j'], panes: MAIL, predicates: [KEY.LIST_NEXT] },
  { id: 'sidebar.prev', label: 'shortcuts.action.sidebarPrev', group: 'sidebar', combos: ['ArrowUp', 'k'], panes: MAIL, predicates: [KEY.LIST_PREV] },
  { id: 'sidebar.toggle', label: 'shortcuts.action.sidebarToggle', group: 'sidebar', combos: ['Enter', 'Space'], panes: MAIL, inline: app('Enter', ' ') },

  // Message list (list focused)
  { id: 'list.next', label: 'shortcuts.action.listNext', group: 'messageList', combos: ['ArrowDown', 'j'], panes: MAIL, predicates: [KEY.LIST_NEXT] },
  { id: 'list.prev', label: 'shortcuts.action.listPrev', group: 'messageList', combos: ['ArrowUp', 'k'], panes: MAIL, predicates: [KEY.LIST_PREV] },
  { id: 'list.first', label: 'shortcuts.action.listFirst', group: 'messageList', combos: ['g'], panes: MAIL, predicates: [KEY.LIST_FIRST] },
  { id: 'list.last', label: 'shortcuts.action.listLast', group: 'messageList', combos: ['Shift+g'], panes: MAIL, predicates: [KEY.LIST_LAST] },
  { id: 'list.nextCheck', label: 'shortcuts.action.listNextCheck', group: 'messageList', combos: ['Shift+ArrowDown', 'Shift+j'], panes: MAIL, predicates: [KEY.LIST_NEXT_CHECK] },
  { id: 'list.prevCheck', label: 'shortcuts.action.listPrevCheck', group: 'messageList', combos: ['Shift+ArrowUp', 'Shift+k'], panes: MAIL, predicates: [KEY.LIST_PREV_CHECK] },
  { id: 'list.toggleCheck', label: 'shortcuts.action.listToggleCheck', group: 'messageList', combos: ['Space', 'x'], panes: MAIL, predicates: [KEY.LIST_TOGGLE_CHECK], inline: app(' ', 'x') },
  { id: 'list.open', label: 'shortcuts.action.listOpen', group: 'messageList', combos: ['Enter', 'o'], panes: MAIL, predicates: [KEY.LIST_OPEN], inline: app('Enter', 'o') },
  { id: 'list.selectAll', label: 'shortcuts.action.listSelectAll', group: 'messageList', combos: ['Mod+a'], panes: MAIL, predicates: [KEY.LIST_SELECT_ALL], inline: app('a') },
  { id: 'list.contextMenu', label: 'shortcuts.action.contextMenu', group: 'messageList', combos: ['ContextMenu', 'RightAlt'], panes: MAIL, inline: app('ContextMenu', 'Alt', 'AltRight') },
  { id: 'list.escape', label: 'shortcuts.action.escape', group: 'messageList', combos: ['Escape', 'u'], panes: MAIL, inline: app('Escape', 'u') },

  // Message actions (checked messages, else the keyboard-focused message)
  { id: 'mail.reply', icon: 'mdi:reply', label: 'shortcuts.action.reply', group: 'messageActions', combos: ['Mod+r', 'r'], panes: MAIL, inline: app('r') },
  { id: 'mail.replyAll', icon: 'mdi:reply-all', label: 'shortcuts.action.replyAll', group: 'messageActions', combos: ['Mod+Shift+r', 'a'], panes: MAIL, inline: app('a', 'r') },
  { id: 'mail.forward', icon: 'mdi:share', label: 'shortcuts.action.forward', group: 'messageActions', combos: ['Mod+f', 'f'], panes: MAIL, inline: app('f') },
  { id: 'mail.archive', icon: 'mdi:archive-outline', label: 'shortcuts.action.archive', group: 'messageActions', combos: ['e', 'Mod+k'], panes: MAIL, inline: app('e', 'k') },
  { id: 'mail.spam', icon: 'mdi:alert-octagon-outline', label: 'shortcuts.action.spam', group: 'messageActions', combos: ['!', 'Mod+j'], panes: MAIL, inline: app('!', 'j') },
  { id: 'mail.trash', icon: 'mdi:delete-outline', label: 'shortcuts.action.trash', group: 'messageActions', combos: ['#', 'Delete', 'Backspace', 'd'], panes: MAIL, inline: app('#', 'Delete', 'Backspace', 'd') },
  { id: 'mail.deletePermanently', icon: 'mdi:delete-forever', label: 'shortcuts.action.deletePermanently', group: 'messageActions', combos: ['Shift+Delete', 'Shift+Backspace'], panes: MAIL, inline: app('Delete', 'Backspace') },
  { id: 'mail.star', icon: 'mdi:star-outline', label: 'shortcuts.action.star', group: 'messageActions', combos: ['s'], panes: MAIL, inline: app('s') },
  { id: 'mail.markRead', icon: 'mdi:email-open-outline', label: 'shortcuts.action.markRead', group: 'messageActions', combos: ['Shift+i', 'Mod+u'], panes: MAIL, inline: app('I', 'u') },
  { id: 'mail.markUnread', icon: 'mdi:email-outline', label: 'shortcuts.action.markUnread', group: 'messageActions', combos: ['Shift+u', 'Mod+Shift+u'], panes: MAIL, inline: app('U', 'u') },
  { id: 'mail.toggleRead', icon: 'mdi:email-sync-outline', label: 'shortcuts.action.toggleRead', group: 'messageActions', combos: ['m'], panes: MAIL, predicates: [KEY.LIST_TOGGLE_READ], inline: app('m') },
  { id: 'mail.moveTo', icon: 'mdi:folder-move-outline', label: 'shortcuts.action.moveTo', group: 'messageActions', combos: ['v', 'Alt+m'], panes: MAIL, predicates: [KEY.LIST_MOVE_TO], inline: app('v') },
  { id: 'mail.copyTo', icon: 'mdi:content-copy', label: 'shortcuts.action.copyTo', group: 'messageActions', combos: ['Alt+c'], panes: MAIL, predicates: [KEY.LIST_COPY_TO] },

  // Conversation viewer (viewer focused)
  { id: 'viewer.scrollDown', label: 'shortcuts.action.scrollDown', group: 'viewer', combos: ['ArrowDown', 'j'], panes: MAIL, predicates: [KEY.LIST_NEXT] },
  { id: 'viewer.scrollUp', label: 'shortcuts.action.scrollUp', group: 'viewer', combos: ['ArrowUp', 'k'], panes: MAIL, predicates: [KEY.LIST_PREV] },
  { id: 'viewer.threadFocus', icon: 'mdi:fullscreen', label: 'shortcuts.action.threadFocus', group: 'viewer', combos: ['Shift+f'], panes: MAIL, inline: app('F') },
  { id: 'viewer.messageFocus', label: 'shortcuts.action.messageFocus', group: 'viewer', combos: ['Shift+f'], panes: MAIL, inline: app('F') },
  { id: 'viewer.selectText', label: 'shortcuts.action.selectText', group: 'viewer', combos: ['Mod+a'], panes: MAIL, inline: app('a') },
  { id: 'viewer.loadImages', label: 'shortcuts.action.loadImages', group: 'viewer', combos: ['Mod+l'], panes: MAIL, inline: app('l') },
  { id: 'viewer.alwaysLoadImages', label: 'shortcuts.action.alwaysLoadImages', group: 'viewer', combos: ['Mod+Shift+l'], panes: MAIL, inline: app('l') },

  // Composer (only while the composer is open)
  { id: 'composer.send', icon: 'mdi:send', label: 'shortcuts.action.send', group: 'composer', combos: ['Mod+Enter'], panes: MAIL, inline: composer('Enter') },
  { id: 'composer.attach', icon: 'mdi:paperclip', label: 'shortcuts.action.attach', group: 'composer', combos: ['Alt+a'], panes: MAIL, inline: composer('a') },
  { id: 'composer.detach', label: 'shortcuts.action.detach', group: 'composer', combos: ['Mod+d'], panes: MAIL, inline: composer('d') },
  { id: 'composer.toolbar', label: 'shortcuts.action.toolbar', group: 'composer', combos: ['Alt+t'], panes: MAIL, inline: composer('t') },
  { id: 'composer.pgp', label: 'shortcuts.action.pgp', group: 'composer', combos: ['Alt+p'], panes: MAIL, inline: composer('p', 's', 'S', 'e', 'E') },
  { id: 'composer.smime', label: 'shortcuts.action.smime', group: 'composer', combos: ['Alt+s'], panes: MAIL, inline: composer('s') },
  { id: 'composer.bold', label: 'shortcuts.action.bold', group: 'composer', combos: ['Mod+b'], panes: MAIL, external: 'TipTap' },
  { id: 'composer.italic', label: 'shortcuts.action.italic', group: 'composer', combos: ['Mod+i'], panes: MAIL, external: 'TipTap' },
  { id: 'composer.underline', label: 'shortcuts.action.underline', group: 'composer', combos: ['Mod+u'], panes: MAIL, external: 'TipTap' },
  { id: 'composer.undo', icon: 'mdi:undo', label: 'shortcuts.action.undo', group: 'composer', combos: ['Mod+z'], panes: MAIL, inline: composer('z') },
  { id: 'composer.redo', icon: 'mdi:redo', label: 'shortcuts.action.redo', group: 'composer', combos: ['Mod+Shift+z'], panes: MAIL, inline: composer('z') },
  { id: 'composer.close', label: 'shortcuts.action.closeComposer', group: 'composer', combos: ['Escape', 'Mod+w'], panes: MAIL, inline: composer('Escape', 'w') },

  // Kit list panes used by extensions (Calendar agenda, Contacts list)
  { id: 'lists.next', label: 'shortcuts.action.listNext', group: 'lists', combos: ['ArrowDown', 'j'], panes: ['calendar', 'contacts'], predicates: [KEY.LIST_NEXT] },
  { id: 'lists.prev', label: 'shortcuts.action.listPrev', group: 'lists', combos: ['ArrowUp', 'k'], panes: ['calendar', 'contacts'], predicates: [KEY.LIST_PREV] },
  { id: 'lists.first', label: 'shortcuts.action.listFirst', group: 'lists', combos: ['g'], panes: ['calendar', 'contacts'], predicates: [KEY.LIST_FIRST] },
  { id: 'lists.last', label: 'shortcuts.action.listLast', group: 'lists', combos: ['Shift+g'], panes: ['calendar', 'contacts'], predicates: [KEY.LIST_LAST] },
  { id: 'lists.open', label: 'shortcuts.action.listOpen', group: 'lists', combos: ['Enter', 'o'], panes: ['calendar', 'contacts'], predicates: [KEY.LIST_OPEN] },
  { id: 'lists.delete', label: 'shortcuts.action.listDelete', group: 'lists', combos: ['Delete', 'Backspace'], panes: ['contacts'], predicates: [KEY.LIST_DELETE] },
]

// Extension registries, discovered by file drop (see header comment).
const extensionModules = import.meta.glob<{ default: ExtensionShortcutRegistry }>(
  '../../../../extensions/*/frontend/keyboard/registry.ts',
  { eager: true },
)

export const EXTENSION_REGISTRIES: ExtensionShortcutRegistry[] = Object.values(extensionModules)
  .map(mod => mod.default)
  .filter((reg): reg is ExtensionShortcutRegistry => !!reg)
  .sort((a, b) => a.extensionId.localeCompare(b.extensionId))

export const SHORTCUT_GROUPS: ShortcutGroupDef[] = [
  ...CORE_GROUPS,
  ...EXTENSION_REGISTRIES.flatMap(reg => reg.groups),
]

export const SHORTCUTS: ShortcutDef[] = [
  ...CORE_SHORTCUTS,
  ...EXTENSION_REGISTRIES.flatMap(reg => reg.shortcuts.map(def => ({
    ...def,
    panes: def.panes ?? [reg.extensionId],
  }))),
]

const byId = new Map(SHORTCUTS.map(def => [def.id, def]))

/** Look up a shortcut by id; undefined for unknown ids. */
export function getShortcut(id: string): ShortcutDef | undefined {
  return byId.get(id)
}

/** All entries of a group, in registry order (e.g. 'messageActions' for an action toolbar). */
export function getShortcuts(group: string): ShortcutDef[] {
  return SHORTCUTS.filter(def => def.group === group)
}

/** True when the shortcut is live in the given rail pane. */
export function appliesToPane(def: ShortcutDef, pane: string): boolean {
  return !def.panes || def.panes.includes(pane)
}
