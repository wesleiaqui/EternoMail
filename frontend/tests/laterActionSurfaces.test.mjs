import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
const read = p => readFile(new URL(p, import.meta.url), 'utf8')
const base = '../src/lib/components/'
const viewer = await read(base+'viewer/ConversationViewer.svelte')
const row = await read(base+'list/ConversationRow.svelte')
const list = await read(base+'list/MessageList.svelte')
// Extracted component functions call this new shared focus setter. Supply its
// real implementation (including the account-qualified key) to the harness.
const focusTs = (await import('typescript')).default
const keySource = await read(base+'list/focusedRow.ts')
const focusSource = list.slice(list.indexOf('  function focusConversation('), list.indexOf('  // Reader/history restoration'))
const focusSupport = 'let focusedRowIdentity=null; const accountId="unified",folderId="inbox";' +
  focusTs.transpileModule(keySource.replaceAll('export ', '') + focusSource, {compilerOptions: {target: focusTs.ScriptTarget.ES2022}}).outputText

const menu = await read(base+'common/MessageContextMenu.svelte')
const button = await read(base+'common/LaterQueueButton.svelte')
test('reader toolbar, per-message header and row quick actions include one Later action', () => {
  assert.equal((viewer.match(/<LaterQueueButton /g)||[]).length, 3)
  assert.equal((viewer.match(/kind="replyLater"/g)||[]).length, 0)
  assert.equal((row.match(/<LaterQueueButton /g)||[]).length, 1)
  assert.match(viewer, /conversationId=\{msg.threadId \|\| threadId \|\| ''\}/)
  assert.match(viewer, /accountId=\{msg.accountId \|\| accountId \|\| ''\}/)
})
test('snoozed and multi-selected context menus keep both account-qualified queue actions', () => {
  const queueMenu = menu.slice(menu.indexOf('{#if queueTargets.length > 0}'), menu.indexOf('<!-- Move/Delete actions -->'))
  assert.ok(queueMenu.indexOf("toggleLater('readLater')") < queueMenu.indexOf("folderType === 'snoozed'"))
  assert.ok(!queueMenu.includes("toggleLater('replyLater')"))
  assert.match(row, /laterTargets=\{useMultiSelect \? selectedQueueRefs : undefined\}/)
  assert.equal((list.match(/\{selectedQueueRefs\}/g)||[]).length, 4)
  assert.match(menu, /queueTargets.map\(ref => \(\{ \.\.\.ref \}\)\)/)
  assert.match(menu, /setWithUndo\(ref.accountId, ref.conversationId, inQueue\)/)
})
test('HTML-body menus receive the conversation target and include one Later action', async () => {
  const body = await read(base+'viewer/EmailBody.svelte')
  const message = await read(base+'viewer/ConversationMessage.svelte')
  assert.equal((message.match(/\{conversationId\}/g)||[]).length, 2)
  assert.equal((body.match(/<LaterQueueButton /g)||[]).length, 1)
})
test('queue buttons share existing APIs, labels, ready/busy guards, hints and operation-specific Undo', async () => {
  for (const part of ['laterQueue.setWithUndo(accountId, conversationId, target)', 'undoLaterChange(result)', 'aria-pressed={inQueue}', '!queue.ready', 'withShortcut(label, shortcut)', 'ariaKeyshortcuts(shortcut)']) assert.ok(button.includes(part), part)
  for (const locale of ['en','fi']) {
    const values = JSON.parse(await read('../src/lib/i18n/locales/'+locale+'.json'))
    for (const kind of ['readLater','replyLater']) for (const key of ['add','remove','added','removed','failed']) assert.ok(values[kind][key], `${locale}.${kind}.${key}`)
  }
})

test('unresolved IMAP result queue actions are disabled and excluded from bulk targets', () => {
  assert.match(list, /_queueCanonical: false/)
  assert.match(list, /queueActionsEnabled=\{result._queueCanonical === true\}/)
  assert.match(list, /conversation._queueCanonical !== false/)
  assert.match(row, /conversationId=\{queueActionsEnabled \? backendConversationId : ''\}/)
  assert.match(menu, /if \(!laterActionsEnabled \|\| laterBusy/)
  assert.match(list, /message\?\.accountId !== account \|\| !message.id/)
  assert.match(list, /_canonicalConversationId: canonicalId, _queueCanonical: true/)
  assert.match(list, /_queueCanonical: !!\(msg.threadId \|\| msg.id\)/)
})

test('one failed selected conversation does not skip later targets or lose successful Undo receipts', async () => {
  const ts = (await import('typescript')).default
  const start = menu.indexOf('  async function toggleLater(')
  const end = menu.indexOf('  async function handleUnsnooze()', start)
  const source = menu.slice(start, end) + '\nexport { toggleLater }'
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
  const calls = [], notices = []
  const queue = { load: async () => {}, has: () => false, setWithUndo: async (account, conversation) => {
    calls.push([account,conversation]); if (conversation === 'B') throw new Error('offline')
    return {changed:true,operationId:'op-'+conversation}
  } }
  const factory = new Function('queueTargets','laterActionsEnabled','laterBusy','readLaterQueue','replyLaterQueue','toasts','$_','UndoOperation', outputText.replace(/export \{ toggleLater \};?/, '')+'; return toggleLater;')
  const handler = factory(['A','B','C'].map(conversationId=>({accountId:'one',conversationId})),true,false,queue,{}, {success:(...args)=>notices.push(args),error:(...args)=>notices.push(args)},x=>x,async()=>{})
  await handler('readLater')
  assert.deepEqual(calls.map(c=>c[1]), ['A','B','C'])
  assert.equal(notices.filter(n=>n[0]==='readLater.added').length,2)
  assert.equal(notices.filter(n=>n[0]==='readLater.failed').length,1)
  assert.ok(notices.filter(n=>n[0]==='readLater.added').every(n=>n[1][0].onClick))
})

test('queue readiness never removes ordinary checked message refs and disables mixed selections', async () => {
  const ordinary = list.slice(list.indexOf('  const selectedMessageRefs'), list.indexOf('  const selectedQueueReady'))
  assert.doesNotMatch(ordinary, /_queueCanonical/)
  assert.match(list, /if \(!selectedQueueReady \|\| laterQueueMutationInFlight/)
  assert.equal((list.match(/disabled: !selectedQueueReady/g)||[]).length, 1)
  assert.equal((list.match(/\{selectedQueueReady\}/g)||[]).length,4)
  assert.match(row,/laterActionsEnabled=\{useMultiSelect \? selectedQueueReady : queueActionsEnabled\}/)
  const bar = await read(base+'kit/ActionBar.svelte')
  assert.equal((bar.match(/disabled=\{disabled \|\| action.disabled\}/g)||[]).length,2)
})

test('local server result resolution uses message-id fallback without bypassing identity or race checks', () => {
  const start=list.indexOf('  async function resolveLocalServerConversation')
  const end=list.indexOf('  // Fetch a non-local server result',start)
  const handler=list.slice(start,end)
  assert.match(handler,/const canonicalId = message.threadId \|\| message.id/)
  assert.match(handler,/generation !== serverResultFetchGeneration/)
  assert.match(handler,/message\?\.accountId !== account/)
  assert.match(handler,/!message.id/)
  assert.match(handler,/onConversationSelect\?\.\(canonicalId, folder, account, result\.subject\)/)
})

test('queue readiness and targets use the active source rather than hidden unresolved cache rows', async () => {
  const ts = (await import('typescript')).default
  const source = list.slice(list.indexOf('  const selectedQueueRows'), list.indexOf('  const selectedAllReadLater'))
  const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } })
  const select = new Function('isSearchMode','serverSearchMode','serverSearchResults','searchResults','conversations','checkedThreadIds','accountId','$derived',outputText+'; return {selectedQueueReady,selectedQueueRefs};')
  const valid = {accountId:'one',threadId:'msg-no-thread',messageIds:['msg-no-thread']}
  const unresolved = {...valid,_queueCanonical:false}
  const checked=new Set(['msg-no-thread'])
  const local=select(true,false,[unresolved],[valid],[],checked,'one',x=>x)
  assert.equal(local.selectedQueueReady,true)
  assert.deepEqual(local.selectedQueueRefs,[{accountId:'one',conversationId:'msg-no-thread'}])
  const server=select(true,true,[unresolved],[valid],[],checked,'one',x=>x)
  assert.equal(server.selectedQueueReady,false)
  assert.deepEqual(server.selectedQueueRefs,[])
  const folder=select(false,true,[unresolved],[],[valid],checked,'one',x=>x)
  assert.equal(folder.selectedQueueReady,true)
  assert.deepEqual(folder.selectedQueueRefs,local.selectedQueueRefs)
  const mixed=select(true,true,[unresolved,{...valid,threadId:'resolved',_queueCanonical:true}],[],[],new Set(['msg-no-thread','resolved']),'one',x=>x)
  assert.equal(mixed.selectedQueueReady,false)
})

test('fetching a result with a shared mailbox UID updates only its account-qualified row', async () => {
  const ts = (await import('typescript')).default
  const identitySource=await read(base+'list/serverSearchResults.ts')
  const identityCode=ts.transpileModule(identitySource,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText
  const {serverSearchResultIdentity}=await import(`data:text/javascript;base64,${Buffer.from(identityCode).toString('base64')}`)
  const source=list.slice(list.indexOf('  async function fetchAndSelectServerResult'),list.indexOf('  function isMissingServerSearchResult'))
  const code=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const factory=new Function('serverSearchResults','FetchServerMessage','onConversationSelect','serverSearchResultIdentity',`
    let checkedThreadIds=new Set(); let serverResultFetchGeneration=0, searchQuery='query', searchSource='server', selectedThreadId='', serverSearchError=''; const migrateCheckedServerThread=()=>{};
    ${focusSupport}
    ${code}
    return {run:fetchAndSelectServerResult,rows:()=>serverSearchResults};`)
  const a={accountId:'A',folderId:'inbox-A',threadId:'uid-A',_uid:42,_isLocal:false,_queueCanonical:false}
  const b={accountId:'B',folderId:'inbox-B',threadId:'uid-B',_uid:42,_isLocal:false,_queueCanonical:false}
  const c={...b,folderId:'archive-B',threadId:'uid-C'}
  const selected=[]
  const state=factory([a,b,c],async()=>({id:'message-B',threadId:'thread-B',accountId:'B'}),(...args)=>selected.push(args),serverSearchResultIdentity)
  await state.run(b,'inbox-B','B')
  assert.deepEqual(state.rows()[0],a)
  assert.deepEqual(state.rows()[2],c)
  assert.equal(state.rows()[1].accountId,'B')
  assert.equal(state.rows()[1]._canonicalConversationId,'thread-B')
  assert.equal(state.rows()[1].threadId,'uid-B')
  assert.equal(state.rows()[1]._queueCanonical,true)
  assert.deepEqual(selected,[['thread-B','inbox-B','B',undefined]])
  const wrong=factory([a,b],async()=>({id:'message-A',threadId:'thread-A',accountId:'A'}),()=>assert.fail('must not open mismatched account'),serverSearchResultIdentity)
  await wrong.run(b,'inbox-B','B')
  assert.deepEqual(wrong.rows(),[a,b])
})

test('late local and fetched server opens cannot replace a newer click or keyboard selection', async () => {
  const ts=(await import('typescript')).default
  const source=list.slice(list.indexOf('  async function resolveLocalServerConversation'),list.indexOf('  function isMissingServerSearchResult'))
  const code=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const factory=new Function('serverSearchResults','GetMessage','FetchServerMessage','onConversationSelect','serverSearchResultIdentity',`
    let checkedThreadIds=new Set(); let serverResultFetchGeneration=0, searchQuery='query', searchSource='server', selectedThreadId='rowA';
    const toasts={error:()=>{throw new Error('stale error surfaced')}};
    ${code}
    return {local:resolveLocalServerConversation,remote:fetchAndSelectServerResult,select:id=>selectedThreadId=id,selected:()=>selectedThreadId,rows:()=>serverSearchResults};`)
  for(const mode of ['local','remote']) for(const outcome of ['success','failure']) {
    const a={threadId:'rowA',accountId:'A',folderId:'inbox',_uid:42,messageIds:['messageA'],_queueCanonical:false}
    let complete,fail
    const pending=new Promise((resolve,reject)=>{complete=resolve;fail=reject})
    const opened=[]
    const state=factory([a],()=>pending,()=>pending,(...args)=>opened.push(args),r=>`${r.accountId}:${r.folderId}:${r._uid}`)
    const request=state[mode](a,'inbox','A')
    // The shared selection state is changed by mouse, j/k, first/last and notifications.
    state.select('rowB')
    if(outcome==='success') complete({id:'messageA',threadId:'threadA',accountId:'A'})
    else fail(new Error('stale lookup failed'))
    await request
    assert.deepEqual(opened,[],`${mode}/${outcome}`)
    assert.equal(state.selected(),'rowB')
    assert.deepEqual(state.rows(),[a])
  }
  const keyboard=list.slice(list.indexOf('  export function openSelected()'),list.indexOf('  // Select a specific thread by ID'))
  assert.match(keyboard,/openConversation\(activeList\[index\] as any\)/)
})

test('keyboard opening preserves checked rows and resolves server rows through shared reader path', async () => {
  const ts=(await import('typescript')).default
  const shared=list.slice(list.indexOf('  function openConversation('),list.indexOf('  async function resolveLocalServerConversation'))
  const keyboard=list.slice(list.indexOf('  export function openSelected()'),list.indexOf('  // Select a specific thread by ID')).replace('export function','function')
  const code=ts.transpileModule(shared+keyboard,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const factory=new Function('activeList','onConversationSelect','resolveLocalServerConversation','fetchAndSelectServerResult',`
    const pinInboxGroupForConversation=id=>{}; let serverResultFetchGeneration=0, selectedThreadId='A', checkedThreadIds=new Set(['A','B']);
    const isUnifiedView=false,isVirtualFolderView=false,isSearchMode=true,folderId='inbox',accountId='one',searchSource='server';
    const getSelectedIndex=()=>0;
    ${code}
    return {open:openSelected,checks:()=>[...checkedThreadIds],generation:()=>serverResultFetchGeneration};`)
  for(const kind of ['ordinary','local','remote']) {
    const calls=[]
    const row={threadId:'A',accountId:'one',folderId:'inbox',_uid:42,...(kind==='local'?{_isLocal:true,_queueCanonical:false}:kind==='remote'?{_isLocal:false}:{})}
    const state=factory([row],(...args)=>calls.push(['reader',...args]),()=>calls.push(['local']),()=>calls.push(['remote']))
    state.open()
    assert.deepEqual(state.checks(),['A','B'])
    assert.equal(state.generation(),1)
    assert.equal(calls[0][0],kind==='ordinary'?'reader':kind)
  }
})

test('canonical server resolution carries checked IDs forward and opening pins inbox cards', async () => {
  const ts=(await import('typescript')).default
  const source=list.slice(list.indexOf('  async function resolveLocalServerConversation'),list.indexOf('  function isMissingServerSearchResult'))
  const code=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const factory=new Function('serverSearchResults',`
    let serverResultFetchGeneration=0, searchQuery='query', searchSource='server',selectedThreadId='provisional';
    let checkedThreadIds=new Set(['provisional','other']); const toasts={error:()=>{}};
    const GetMessage=async()=>({accountId:'one',id:'message',threadId:'canonical'}),FetchServerMessage=GetMessage;
    const onConversationSelect=()=>{},serverSearchResultIdentity=r=>r.accountId+':'+r._uid;
    ${focusSupport}
    ${code}
    return {local:resolveLocalServerConversation,remote:fetchAndSelectServerResult,checks:()=>[...checkedThreadIds],rows:()=>serverSearchResults};`)
  for(const kind of ['local','remote']) {
    const row={threadId:'provisional',accountId:'one',folderId:'inbox',_uid:42,messageIds:['message']}
    const state=factory([row]); await state[kind](row,'inbox','one')
    assert.deepEqual(state.checks(),['provisional','other'])
    assert.equal(state.rows()[0]._canonicalConversationId,'canonical')
    assert.equal(state.rows()[0].threadId,'provisional')
  }
  const shared=list.slice(list.indexOf('  function openConversation'),list.indexOf('  async function resolveLocalServerConversation'))
  assert.match(shared,/pinInboxGroupForConversation\(threadId\)/)
  assert.doesNotMatch(shared,/checkedThreadIds = new Set/)
})

test('same-UID server rows keep independent checked keys through canonical resolution', async () => {
  const ts=(await import('typescript')).default
  const identityCode=ts.transpileModule(await read(base+'list/serverSearchResults.ts'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText
  const {serverSearchResultIdentity}=await import(`data:text/javascript;base64,${Buffer.from(identityCode).toString('base64')}`)
  const adapter=list.slice(list.indexOf('  function adaptServerResult'),list.indexOf('  // Toggle search visibility'))
  const resolution=list.slice(list.indexOf('  async function resolveLocalServerConversation'),list.indexOf('  function isMissingServerSearchResult'))
  const code=ts.transpileModule(adapter+resolution,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const factory=new Function('serverSearchResultIdentity',`
    let serverResultFetchGeneration=0,searchQuery='q',searchSource='server',selectedThreadId='',serverSearchResults=[],checkedThreadIds=new Set();
    const FetchServerMessage=async(account)=>({accountId:account,id:'msg-'+account,threadId:'thread-'+account}),onConversationSelect=()=>{};
    ${focusSupport}
    ${code}
    return {adapt:adaptServerResult,open:fetchAndSelectServerResult,setup:rows=>{serverSearchResults=rows;checkedThreadIds=new Set(rows.map(r=>r.threadId));selectedThreadId=rows[0].threadId},checks:()=>checkedThreadIds,rows:()=>serverSearchResults};`)
  const state=factory(serverSearchResultIdentity)
  const rows=[['A','inbox'],['B','inbox'],['A','archive']].map(([accountId,folderId])=>state.adapt({accountId,folderId,uid:42,isLocal:false}))
  assert.equal(new Set(rows.map(r=>r.threadId)).size,3)
  state.setup(rows)
  await state.open(rows[0],'inbox','A')
  assert.equal(state.checks().size,3)
  assert.equal(state.checks().has(rows[0].threadId),true)
  assert.equal(state.checks().has(rows[1].threadId),true)
  assert.equal(state.checks().has(rows[2].threadId),true)
  assert.deepEqual(state.rows().filter(r=>state.checks().has(r.threadId)).map(r=>r.accountId),['A','B','A'])
  assert.equal(state.rows()[1]._queueCanonical,false)
  assert.equal(state.rows()[2]._queueCanonical,false)
})

test('same canonical RFC thread in two accounts never checks or queues the unselected account', async () => {
  const ts=(await import('typescript')).default
  const identityCode=ts.transpileModule(await read(base+'list/serverSearchResults.ts'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText
  const {serverSearchResultIdentity}=await import(`data:text/javascript;base64,${Buffer.from(identityCode).toString('base64')}`)
  const adapter=list.slice(list.indexOf('  function adaptServerResult'),list.indexOf('  // Toggle search visibility'))
  const resolution=list.slice(list.indexOf('  async function resolveLocalServerConversation'),list.indexOf('  function isMissingServerSearchResult'))
  const derived=list.slice(list.indexOf('  const selectedQueueRows'),list.indexOf('  const selectedAllReadLater'))
  const code=ts.transpileModule(adapter+resolution,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const queueCode=ts.transpileModule(derived,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const factory=new Function('serverSearchResultIdentity',`
    let serverResultFetchGeneration=0,searchQuery='q',searchSource='server',selectedThreadId='',serverSearchResults=[],checkedThreadIds=new Set();
    const FetchServerMessage=async(account)=>({accountId:account,id:'msg-'+account,threadId:'root@example.org'}),GetMessage=async()=>({accountId:'A',id:'msg-A',threadId:'root@example.org'}),onConversationSelect=()=>{},toasts={error:()=>{}};
    ${focusSupport}
    ${code}
    return {adapt:adaptServerResult,local:resolveLocalServerConversation,remote:fetchAndSelectServerResult,
      setup:rows=>{serverSearchResults=rows;checkedThreadIds=new Set([rows[0].threadId]);selectedThreadId=rows[0].threadId},
      checks:()=>checkedThreadIds,rows:()=>serverSearchResults,
      targets:()=>{const isSearchMode=true,serverSearchMode=true,conversations=[],searchResults=[],accountId='unified',$derived=x=>x;${queueCode};return selectedQueueRefs}};`)
  for(const kind of ['local','remote']) {
    const state=factory(serverSearchResultIdentity)
    const a=state.adapt({accountId:'A',folderId:'inbox-A',uid:42,isLocal:kind==='local',messageId:kind==='local'?'msg-A':undefined})
    const b={...state.adapt({accountId:'B',folderId:'inbox-B',uid:42,isLocal:true,messageId:'msg-B'}),_canonicalConversationId:'root@example.org',_queueCanonical:true}
    state.setup([a,b]); await state[kind](a,'inbox-A','A')
    assert.equal(state.rows()[0]._canonicalConversationId,'root@example.org')
    assert.equal(state.rows()[1]._canonicalConversationId,'root@example.org')
    assert.equal(state.checks().size,1)
    assert.equal(state.checks().has(b.threadId),false)
    assert.deepEqual(state.targets(),[{accountId:'A',conversationId:'root@example.org'}])
  }
  assert.match(row,/conversationId=\{useMultiSelect \? undefined : backendConversationId\}/)
  assert.match(row,/OpenReaderWindow\(accountId, folderId, backendConversationId/)
})

test('reader restoration preserves selected same-thread UID and matches both account and folder', async () => {
  const ts=(await import('typescript')).default
  const source=list.slice(list.indexOf('  function serverRowForReaderSelection'),list.indexOf('  $effect(() => {\n    const selection = readerSelection'))
  const code=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText
  const resolve=new Function(code+';return serverRowForReaderSelection;')()
  const first={threadId:'uid-41',_canonicalConversationId:'T',accountId:'A',folderId:'inbox'}
  const second={...first,threadId:'uid-42'}
  const folder={...first,threadId:'uid-43',folderId:'archive'}
  const account={...first,threadId:'uid-44',accountId:'B'}
  const rows=[first,second,folder,account]
  assert.equal(resolve(rows,{threadId:'T',accountId:'A',folderId:'inbox'},'uid-42'),second)
  assert.equal(resolve(rows,{threadId:'T',accountId:'A',folderId:'inbox'},'uid-43'),first)
  assert.equal(resolve(rows,{threadId:'T',accountId:'B',folderId:'inbox'},'uid-42'),account)
  assert.equal(resolve(rows,{threadId:'T',accountId:'A',folderId:'archive'},'uid-42'),folder)
  assert.equal(resolve(rows,{threadId:'T',accountId:'C',folderId:'inbox'},null),undefined)
  assert.match(list,/serverRowForReaderSelection\(serverSearchResults, selection, untrack\(\(\) => selectedThreadId\)\)/)
})
