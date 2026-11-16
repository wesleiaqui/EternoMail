import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync,readdirSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import {join} from 'node:path'
const root=new URL('../../',import.meta.url)
const catalog=JSON.parse(readFileSync(new URL('internal/nativemenu/catalog.json',root),'utf8'))
function files(dir){return readdirSync(dir,{withFileTypes:true}).flatMap(entry=>entry.isDirectory()?files(join(dir,entry.name)):/\.(svelte|ts)$/.test(entry.name)?[join(dir,entry.name)]:[])}
const sources=[...files(fileURLToPath(new URL('frontend/src',root))),...files(fileURLToPath(new URL('extensions',root)))].map(path=>readFileSync(path,'utf8')).join('\n')
function ids(items){return items.flatMap(item=>[...(item.id?[item.id]:[]),...ids(item.children??[])])}
test('every native menu id has a frontend route and every explicit control id exists in the catalog',()=>{
  const known=new Set(ids(catalog))
  for(const id of known)assert.ok(sources.includes(id),`unrouted catalog action ${id}`)
  for(const [,id] of sources.matchAll(/data-native-action="([^"]+)"/g))assert.ok(known.has(id),`unknown UI route ${id}`)
  assert.equal(known.size>100,true)
})
test('every window bootstrap installs native dispatch before mounting its component',()=>{
  for(const file of ['main.ts','composerMain.ts','reader.ts','mailWindow.ts']){
    const source=readFileSync(new URL(`frontend/src/${file}`,root),'utf8')
    assert.ok(source.indexOf('installNativeMenu()')<source.lastIndexOf('mount('),file)
  }
})
