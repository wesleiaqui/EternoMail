import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const emailBody = await readFile(new URL('../src/lib/components/viewer/EmailBody.svelte', import.meta.url), 'utf8')
const nativeMenu = await readFile(new URL('../../main.go', import.meta.url), 'utf8')
const editCommand = await readFile(new URL('../../app/undo.go', import.meta.url), 'utf8')

test('copy from a selected sandboxed email body uses the native clipboard bridge', () => {
  assert.match(emailBody, /e\.key\.toLowerCase\(\) === 'c'/)
  assert.match(emailBody, /var text = selectedText \? selectedText\.toString\(\) : ''/)
  assert.match(emailBody, /e\.preventDefault\(\);\s*window\.parent\.postMessage\(\{ type: 'copy-selected-text', text: text \}/)
  assert.match(emailBody, /event\.source !== iframeElement\?\.contentWindow/)
  assert.match(emailBody, /ClipboardSetText\(event\.data\.text\)/)
})

test('native Edit shortcuts are registered and target the focused sandboxed frame', () => {
  assert.match(nativeMenu, /menu\.Text\("Cut", keys\.CmdOrCtrl\("x"\)/)
  assert.match(nativeMenu, /menu\.Text\("Copy", keys\.CmdOrCtrl\("c"\)/)
  // Keep Cmd+V native to WebKit so composer editors receive a real paste
  // event (including rich content and images); the menu item remains a
  // click-only text fallback.
  assert.match(nativeMenu, /menu\.Text\("Paste", nil, func\(\*menu\.CallbackData\) \{ application\.RunEditMenuCommand\("paste"\) \}\)/)
  assert.match(nativeMenu, /menu\.Text\("Select All", keys\.CmdOrCtrl\("a"\)/)
  assert.match(editCommand, /active\.contentWindow\.postMessage\(\{ type: 'native-edit-command', command:/)
  assert.ok(editCommand.includes(`document.execCommand('"+command+"')`))
})
