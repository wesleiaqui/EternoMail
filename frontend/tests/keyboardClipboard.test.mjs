import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const emailBody = await readFile(new URL('../src/lib/components/viewer/EmailBody.svelte', import.meta.url), 'utf8')
const nativeMenu = await readFile(new URL('../../internal/nativemenu/menu.go', import.meta.url), 'utf8')
const dispatcher = await readFile(new URL('../src/lib/nativeMenu.ts', import.meta.url), 'utf8')
const editCommand = await readFile(new URL('../../app/undo.go', import.meta.url), 'utf8')

test('copy from a selected sandboxed email body uses the native clipboard bridge', () => {
  assert.match(emailBody, /e\.key\.toLowerCase\(\) === 'c'/)
  assert.match(emailBody, /var text = selectedText \? selectedText\.toString\(\) : ''/)
  assert.match(emailBody, /e\.preventDefault\(\);\s*window\.parent\.postMessage\(\{ type: 'copy-selected-text', text: text \}/)
  assert.match(emailBody, /event\.source !== iframeElement\?\.contentWindow/)
  assert.match(emailBody, /ClipboardSetText\(event\.data\.text\)/)
})

test('native Edit shortcuts are registered and target the focused sandboxed frame', () => {
  for (const [id,key] of [['cut','x'],['copy','c'],['paste','v'],['selectAll','a']]) {
    assert.ok(nativeMenu.includes(`case "edit.${id}":`))
    assert.ok(nativeMenu.includes(`accelerator = keys.CmdOrCtrl("${key}")`))
  }
  assert.match(dispatcher, /const text=await ClipboardGetText\(\)/)
  assert.match(dispatcher, /if\(root\.activeElement===active && before===after\) insertClipboardTextAtFocus\(text\)/)
  assert.match(dispatcher, /active\.contentWindow\.postMessage\(\{type:'native-edit-command',command\}/)
  assert.match(dispatcher, /root\.execCommand\(command\)/)
  assert.match(editCommand, /active\.contentWindow\.postMessage\(\{ type: 'native-edit-command', command:/)
  assert.ok(editCommand.includes(`document.execCommand('"+command+"')`))
})

test('detached composer preserves native image and rich-text paste',async()=>{
  assert.match(nativeMenu,/if nativeEdit && nativeClipboard\(id\)/)
  const objc=await readFile(new URL('../../internal/nativemenu/clipboard_darwin.m',import.meta.url),'utf8')
  assert.match(objc,/@selector\(paste:\)/)
  assert.match(objc,/dispatch_async\(dispatch_get_main_queue\(\)/)
  assert.doesNotMatch(objc,/@selector\((undo|redo):\)/)
  const main=await readFile(new URL('../../main.go',import.meta.url),'utf8')
  assert.match(main.slice(main.indexOf('func runComposerMode()')),/nativemenu\.NewComposer\(\)/)
})
