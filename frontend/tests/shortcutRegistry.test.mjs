// Keeps the keyboard shortcut registry (src/lib/keyboard/registry.ts and
// extensions/*/frontend/keyboard/registry.ts) in step with the real bindings.
// See the header of registry.ts for what each check guards against.
//
// Modules are loaded through Vite's SSR loader so the $lib / $extensions
// aliases and import.meta.glob resolve exactly as they do in the app.

import { test, after } from 'node:test'
import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { createServer } from 'vite'

const frontendDir = fileURLToPath(new URL('..', import.meta.url))
const repoDir = path.resolve(frontendDir, '..')

const server = await createServer({
  root: frontendDir,
  configFile: path.join(frontendDir, 'vite.config.ts'),
  logLevel: 'error',
  server: { middlewareMode: true, hmr: false, watch: null },
  appType: 'custom',
  optimizeDeps: { noDiscovery: true, include: [] },
})
after(() => server.close())

const registry = await server.ssrLoadModule('/src/lib/keyboard/registry.ts')
const format = await server.ssrLoadModule('/src/lib/keyboard/format.ts')
const shortcutModule = await server.ssrLoadModule('/src/lib/keyboard/shortcuts.ts')
const coreKeys = shortcutModule.KEY

const { SHORTCUTS, SHORTCUT_GROUPS, CORE_SHORTCUTS, CORE_GROUPS, EXTENSION_REGISTRIES } = registry

// --- synthetic events -------------------------------------------------------

const NAMED_KEYS = {
  Space: ' ',
  ArrowUp: 'ArrowUp',
  ArrowDown: 'ArrowDown',
  ArrowLeft: 'ArrowLeft',
  ArrowRight: 'ArrowRight',
  Enter: 'Enter',
  Escape: 'Escape',
  Tab: 'Tab',
  Backspace: 'Backspace',
  Delete: 'Delete',
}

// Each displayed combo becomes one or more events: 'Mod' expands to a Ctrl
// variant and a Cmd variant because the overlay promises both work.
function eventsFor(combo) {
  const { mods, key } = format.parseCombo(combo)
  const shift = mods.includes('Shift') || key === '?'
  const base = {
    key: NAMED_KEYS[key] ?? (key.length === 1 && shift ? key.toUpperCase() : key),
    code: '',
    shiftKey: shift,
    altKey: mods.includes('Alt'),
    ctrlKey: mods.includes('Ctrl'),
    metaKey: mods.includes('Meta'),
  }
  if (!mods.includes('Mod')) return [base]
  return [{ ...base, ctrlKey: true }, { ...base, metaKey: true }]
}

// --- source scanning for inline bindings ------------------------------------

async function handlerBody(file, start, end) {
  const source = await readFile(path.join(frontendDir, file), 'utf8')
  const from = source.indexOf(start)
  assert.ok(from >= 0, `${file}: cannot find "${start}"`)
  const to = source.indexOf(end, from + start.length)
  assert.ok(to > from, `${file}: cannot find "${end}" after "${start}"`)
  return source.slice(from, to)
}

// `switch (focusedPane)` case labels in the handler, not keys.
const PANE_CASES = new Set(['sidebar', 'messageList', 'viewer'])

function keyLiterals(body) {
  const found = new Set()
  for (const m of body.matchAll(/case '([^']+)':/g)) {
    if (!PANE_CASES.has(m[1])) found.add(m[1])
  }
  for (const m of body.matchAll(/e\.(?:key|code)(?:\.toLowerCase\(\))? === '([^']+)'/g)) found.add(m[1])
  return found
}

const handlers = {
  'App.svelte': await handlerBody('src/App.svelte', 'function handleGlobalKeyDown(', 'function getLastMessageId('),
  'Composer.svelte': await handlerBody('src/lib/components/composer/Composer.svelte', 'function handleKeyDown(', 'function generateCID('),
}

// --- locale lookup ----------------------------------------------------------

function lookup(dict, dotted) {
  return dotted.split('.').reduce((node, part) => (node && typeof node === 'object' ? node[part] : undefined), dict)
}

async function loadLocale(file) {
  return JSON.parse(await readFile(file, 'utf8'))
}

// --- tests ------------------------------------------------------------------

test('shortcut ids are unique and reference known groups', () => {
  const ids = new Set()
  const groups = new Set(SHORTCUT_GROUPS.map(g => g.id))
  for (const def of SHORTCUTS) {
    assert.ok(!ids.has(def.id), `duplicate shortcut id ${def.id}`)
    ids.add(def.id)
    assert.ok(groups.has(def.group), `${def.id}: unknown group ${def.group}`)
    assert.ok(def.combos.length > 0, `${def.id}: no combos`)
    const kinds = [def.predicates?.length > 0, !!def.inline, !!def.external, !!def.dispatch].filter(Boolean).length
    assert.ok(kinds > 0, `${def.id}: needs predicates, inline or external binding`)
  }
})

test('every displayed combo satisfies one of the entry predicates (Ctrl and Cmd for Mod)', () => {
  for (const def of SHORTCUTS) {
    if (!def.predicates?.length) continue
    for (const combo of def.combos) {
      for (const ev of eventsFor(combo)) {
        assert.ok(def.predicates.some(p => p(ev)), `${def.id}: "${combo}" (${JSON.stringify(ev)}) matches no predicate`)
      }
    }
    for (const [i, p] of def.predicates.entries()) {
      const hit = def.combos.some(combo => eventsFor(combo).some(ev => p(ev)))
      assert.ok(hit, `${def.id}: predicate #${i} accepts none of the displayed combos`)
    }
  }
})

test('every exported predicate is described in the registry', () => {
  const used = new Set(SHORTCUTS.flatMap(def => def.predicates ?? []))
  const maps = [['$lib/keyboard/shortcuts KEY', coreKeys], ...EXTENSION_REGISTRIES.map(r => [`${r.extensionId} KEY`, r.predicates])]
  for (const [where, map] of maps) {
    for (const [name, fn] of Object.entries(map)) {
      assert.ok(used.has(fn), `${where}.${name} is bound but not shown in the shortcuts overlay`)
    }
  }
})

test('every extension with a keyboard/shortcuts.ts ships a registry', async () => {
  const dirs = await readdir(path.join(repoDir, 'extensions'), { withFileTypes: true })
  const ids = new Set(EXTENSION_REGISTRIES.map(r => r.extensionId))
  for (const dir of dirs.filter(d => d.isDirectory())) {
    const files = await readdir(path.join(repoDir, 'extensions', dir.name, 'frontend', 'keyboard')).catch(() => [])
    if (!files.includes('shortcuts.ts')) continue
    assert.ok(files.includes('registry.ts'), `extensions/${dir.name} has keyboard/shortcuts.ts but no registry.ts`)
    assert.ok(ids.has(dir.name), `extensions/${dir.name}/frontend/keyboard/registry.ts not discovered`)
  }
})

test('inline key literals exist in their handler, and every handler literal is described', () => {
  for (const [file, body] of Object.entries(handlers)) {
    const found = keyLiterals(body)
    const declared = new Set()
    for (const def of SHORTCUTS) {
      if (def.inline?.file !== file) continue
      for (const key of def.inline.keys) {
        declared.add(key)
        assert.ok(found.has(key), `${def.id}: key '${key}' is no longer bound in ${file}`)
      }
    }
    for (const key of found) {
      assert.ok(declared.has(key), `${file} binds key '${key}' but no registry entry describes it`)
    }
  }
})

test('labels and group headings exist in en and fi', async () => {
  const core = {
    en: await loadLocale(path.join(frontendDir, 'src/lib/i18n/locales/en.json')),
    fi: await loadLocale(path.join(frontendDir, 'src/lib/i18n/locales/fi.json')),
  }
  const coreIds = new Set(CORE_SHORTCUTS.map(d => d.id))
  const coreKeysToCheck = [
    ...CORE_GROUPS.map(g => g.label),
    ...CORE_SHORTCUTS.map(d => d.label),
    'shortcuts.title', 'shortcuts.description', 'shortcuts.search', 'shortcuts.showAll', 'shortcuts.noResults', 'shortcuts.or', 'shortcuts.footer', 'shortcuts.open',
  ]
  for (const [lang, dict] of Object.entries(core)) {
    for (const key of coreKeysToCheck) {
      assert.equal(typeof lookup(dict, key), 'string', `core ${lang}.json is missing ${key}`)
    }
  }
  for (const reg of EXTENSION_REGISTRIES) {
    for (const lang of ['en', 'fi']) {
      const dict = await loadLocale(path.join(repoDir, 'extensions', reg.extensionId, 'frontend/i18n/locales', `${lang}.json`))
      const keys = [...reg.groups.map(g => g.label), ...reg.shortcuts.filter(d => !coreIds.has(d.id)).map(d => d.label)]
      for (const key of keys) {
        assert.equal(typeof lookup(dict, key), 'string', `${reg.extensionId} ${lang}.json is missing ${key}`)
      }
    }
  }
})

test('formatting follows the platform', () => {
  assert.equal(format.formatCombo('Mod+Shift+r', false), 'Ctrl+Shift+R')
  assert.equal(format.formatCombo('Mod+Shift+r', true), '⌘⇧R')
  assert.equal(format.formatCombo('Ctrl+Tab', true), '⌃⇥')
  assert.equal(format.formatCombo('Shift+g', false), 'Shift+G')
  assert.equal(format.formatCombo('g', false), 'g')
  assert.equal(format.formatCombo('Alt+ArrowUp', false), 'Alt+↑')
  assert.equal(format.withShortcut('Reply', 'mail.reply', false), 'Reply (Ctrl+R)')
  assert.equal(format.withShortcut('Reply', 'mail.reply', true), 'Reply (⌘R)')
  assert.equal(format.withShortcut('Settings', 'app.preferences', false), 'Settings')
  assert.equal(format.withShortcut('Settings', 'app.preferences', true), 'Settings (⌘,)')
  assert.equal(format.withShortcut('Nope', 'does.not.exist', false), 'Nope')
  assert.equal(format.ariaKeyshortcuts('mail.replyAll', false), 'Control+Shift+R a')
  assert.equal(format.ariaKeyshortcuts('mail.replyAll', true), 'Meta+Shift+R a')
  assert.equal(format.ariaKeyshortcuts('app.preferences', false), undefined)
})

test('Finnish layout symbols support AltGr without swallowing real modifiers', () => {
  const event = (key, modifiers = {}) => ({ key, altKey: false, ctrlKey: false, metaKey: false, shiftKey: false, getModifierState: () => false, ...modifiers })
  assert.equal(shortcutModule.LAYOUT_SYMBOL(event('#', { altKey: true })), true, 'macOS Option+3')
  assert.equal(shortcutModule.LAYOUT_SYMBOL(event('#', { altKey: true, ctrlKey: true, getModifierState: key => key === 'AltGraph' })), true, 'Windows/Linux AltGr+3')
  assert.equal(shortcutModule.LAYOUT_SYMBOL(event('#', { altKey: true, ctrlKey: true })), false, 'actual Ctrl+Alt is not AltGr')
  assert.equal(shortcutModule.LAYOUT_SYMBOL(event('#', { metaKey: true, altKey: true })), false, 'Cmd combinations stay separate')
  assert.equal(shortcutModule.LAYOUT_SYMBOL(event('m', { altKey: true })), false, 'Alt+M remains move')
  assert.equal(coreKeys.SHORTCUTS_HELP(event('?', { altKey: true })), true)
  assert.equal(shortcutModule.PERMANENT_DELETE(event('#', { shiftKey: true })), false, 'typing # must never permanently delete')
  assert.equal(shortcutModule.PERMANENT_DELETE(event('Delete', { shiftKey: true })), true)

})

// The native catalog must show the same primary binding as context menus and
// tooltips. Alternative working bindings must not become a second hint table.
test('native menu shortcut labels match the keyboard registry primary binding', async () => {
  const catalog = JSON.parse(await readFile(path.join(repoDir, 'internal/nativemenu/catalog.json'), 'utf8'))
  const fixed = new Set(['window.new', 'edit.cut', 'edit.copy', 'edit.paste', 'edit.selectAll'])
  // app.search is updated by the separate approved search UI PR, not this fix.
  fixed.add('app.search')
  const walk = items => {
    for (const item of items) {
      if (item.children) walk(item.children)
      if (!item.key || fixed.has(item.id)) continue
      assert.ok(item.shortcutId, `${item.id}: missing registry reference`)
      const def = registry.getShortcut(item.shortcutId)
      assert.ok(def, `${item.id}: unknown registry reference ${item.shortcutId}`)
      assert.equal(item.key, format.formatShortcut(item.shortcutId, true), `${item.id}: native hint drifted from real binding`)
    }
  }
  walk(catalog)
})
