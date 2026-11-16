// Real browser dispatcher and App tests with a deterministic Wails boundary.
// POSTI_PLAYWRIGHT_PATH=/path/to/playwright/index.mjs node --test tests/nativeMenu.browser.test.mjs
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { realpathSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
const browserModule=process.env.POSTI_PLAYWRIGHT_PATH
const captureDir=process.env.POSTI_BROWSER_CAPTURE_DIR||join(tmpdir(),'posti-menu-captures')
mkdirSync(captureDir,{recursive:true})

test('native menu dispatch: stale selections, busy controls, modal isolation, focused rows and edit focus', {skip:!browserModule,timeout:60000},async()=>{
  const server=await createServer({root:fileURLToPath(new URL('..',import.meta.url)),server:{host:'127.0.0.1',port:5188,strictPort:true,fs:{allow:[fileURLToPath(new URL('../..',import.meta.url)),fileURLToPath(new URL('../node_modules',import.meta.url)),realpathSync(fileURLToPath(new URL('../node_modules',import.meta.url)))]}},logLevel:'error'})
  await server.listen()
  const {chromium}=await import(browserModule)
  const browser=await chromium.launch({executablePath:process.env.POSTI_CHROMIUM_BUNDLED?undefined:process.env.CHROME_BIN||'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
  try {
    const page=await browser.newPage()
    await page.route('**/native-menu-test',route=>route.fulfill({contentType:'text/html',body:'<html><body><div id="fixture"></div></body></html>'}))
    await page.goto('http://127.0.0.1:5188/native-menu-test')
    await page.evaluate(async()=>{
      window.listeners=new Map();window.states=[];window.calls=[]
      window.runtime={EventsOnMultiple:(name,callback)=>{window.listeners.set(name,callback);return()=>window.listeners.delete(name)},EventsEmit:(name,state)=>{if(name==='native-menu:state')window.states.push(state)},ClipboardGetText:async()=> 'PASTED'}
      window.menu=await import('/src/lib/nativeMenu.ts')
      window.fixture=document.querySelector('#fixture')
      window.fixture.innerHTML='<div data-native-context="thread-A"><button id="trash" data-native-action="mail.trash">Trash A</button></div>'
      document.querySelector('#trash').onclick=()=>window.calls.push('trash-A')
      window.cancel=window.menu.installNativeMenu()
    })
    const state=()=>page.evaluate(()=>window.states.at(-1))
    const invoke=id=>page.evaluate(id=>window.listeners.get('native-menu:invoke')({...window.states.at(-1),id}),id)
    await page.waitForFunction(()=>window.states.length>0)
    assert.ok((await state()).enabled.includes('mail.trash'))
    await invoke('mail.trash');assert.deepEqual(await page.evaluate(()=>window.calls),['trash-A'])
    // Selection changes are detected synchronously, before the debounce publish.
    await page.evaluate(()=>{const old=window.states.at(-1);document.querySelector('[data-native-context]').dataset.nativeContext='thread-B';document.querySelector('#trash').onclick=()=>window.calls.push('trash-B');window.listeners.get('native-menu:invoke')({...old,id:'mail.trash'})})
    assert.deepEqual(await page.evaluate(()=>window.calls),['trash-A'])
    await page.waitForTimeout(70);await invoke('mail.trash');assert.deepEqual(await page.evaluate(()=>window.calls),['trash-A','trash-B'])
    const before=await state()
    await page.evaluate(()=>{document.querySelector('#trash').textContent='Clock tick'})
    await page.waitForTimeout(70)
    assert.equal((await state()).epoch,before.epoch,'unrelated text updates do not invalidate clicks')
    await invoke('mail.trash');assert.equal((await page.evaluate(()=>window.calls)).length,3)
    await page.evaluate(()=>{document.querySelector('#trash').disabled=true;window.removeRegistration=window.menu.registerMenuAction('mail.trash',()=>window.calls.push('unsafe-fallback'))})
    await page.waitForTimeout(70);assert.ok(!(await state()).enabled.includes('mail.trash'))
    await invoke('mail.trash');assert.equal((await page.evaluate(()=>window.calls)).length,3)
    await page.evaluate(()=>{window.removeRegistration();document.querySelector('#trash').disabled=false;window.fixture.insertAdjacentHTML('beforeend','<div role="dialog"><input id="modal-text"><button data-native-action="calendar.findTime" id="find">Find</button></div>');document.querySelector('#find').onclick=()=>window.calls.push('find')})
    await page.waitForTimeout(70);assert.ok(!(await state()).enabled.includes('mail.trash'));assert.ok((await state()).enabled.includes('calendar.findTime'))
    await invoke('calendar.findTime');assert.equal((await page.evaluate(()=>window.calls)).at(-1),'find')
    await page.evaluate(()=>{window.fixture.innerHTML='<div data-composer-root><button data-native-action="composer.send">Send</button><div role="alertdialog"><button data-native-action="calendar.findTime">Review</button></div></div>'})
    await page.waitForTimeout(70);assert.ok(!(await state()).enabled.includes('composer.send'),'nested confirmation disables Send')
    await page.evaluate(()=>{window.fixture.innerHTML='<div data-native-row><button id="one" data-native-action="attachment.save">Save one</button></div><div data-native-row><button id="two" data-native-action="attachment.save">Save two</button></div>';document.querySelector('#one').onclick=()=>window.calls.push('one');document.querySelector('#two').onclick=()=>window.calls.push('two')})
    await page.waitForTimeout(70);assert.ok(!(await state()).enabled.includes('attachment.save'))
    await page.locator('#two').focus();await page.waitForTimeout(70);await invoke('attachment.save');assert.equal((await page.evaluate(()=>window.calls)).at(-1),'two')
    await invoke('not-in-catalog');assert.equal((await page.evaluate(()=>window.calls)).at(-1),'two')
    await page.evaluate(()=>{window.fixture.innerHTML='<textarea id="editor">before</textarea>'})
    await page.locator('#editor').focus();await page.waitForTimeout(70)
    const editState=await state();await page.waitForTimeout(70);assert.equal((await state()).epoch,editState.epoch,'edit actions have stable target identities')
    await invoke('edit.paste');await page.waitForFunction(()=>document.querySelector('#editor').value.includes('PASTED'))
    assert.equal(await page.locator('#editor').inputValue(),'PASTEDbefore')
    // Clipboard reads are async. Never paste into a different field if focus
    // moved while the native clipboard operation was outstanding.
    await page.evaluate(()=>{window.runtime.ClipboardGetText=()=>new Promise(resolve=>window.clipResolve=resolve);window.fixture.insertAdjacentHTML('beforeend','<textarea id="other"></textarea>')})
    await invoke('edit.paste')
    await page.locator('#other').focus()
    await page.evaluate(()=>window.clipResolve('WRONG FIELD'))
    await page.waitForTimeout(25)
    assert.equal(await page.locator('#other').inputValue(),'')
    assert.equal(await page.locator('#editor').inputValue(),'PASTEDbefore')
    await page.screenshot({path:join(captureDir,'postilaatikko-native-menu-dispatch.png')})
    await page.evaluate(()=>window.cancel())
  } finally {await browser.close();await server.close()}
})

test('real App native menus reuse navigation and isolate Calendar, Contacts and composer', {skip:!browserModule,timeout:90000},async()=>{
  const server=await createServer({root:fileURLToPath(new URL('..',import.meta.url)),server:{host:'127.0.0.1',port:5189,strictPort:true,fs:{allow:[fileURLToPath(new URL('../..',import.meta.url)),fileURLToPath(new URL('../node_modules',import.meta.url)),realpathSync(fileURLToPath(new URL('../node_modules',import.meta.url)))]}},logLevel:'error'})
  await server.listen()
  const {chromium}=await import(browserModule)
  const browser=await chromium.launch({executablePath:process.env.POSTI_CHROMIUM_BUNDLED?undefined:process.env.CHROME_BIN||'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
  try {
    const page=await browser.newPage({viewport:{width:1280,height:900}})
    const errors=[];page.on('pageerror',error=>errors.push(error.message))
    await page.addInitScript(()=>{
      localStorage.setItem('postilaatikko:context-pane-auto-hide','false')
      window.listeners=new Map();window.states=[];window.backendCalls=[]
      window.runtime=new Proxy({}, {get:(_,key)=>key==='EventsOnMultiple'?(name,callback)=>{const list=window.listeners.get(name)||[];list.push(callback);window.listeners.set(name,list);return()=>window.listeners.set(name,list.filter(cb=>cb!==callback))}:key==='EventsEmit'?(name,state)=>{if(name==='native-menu:state')window.states.push(state)}:()=>Promise.resolve()})
      window.data={};const data=window.data;Object.assign(data,{GetComposeMode:{accountId:'demo',mode:'new'},GetWindowDecorationStatus:{effective_mode:'native'},DraftHistoryTree:{nodes:[],draftKey:'fixture',headNodeId:''},IsReady:true,GetThemeMode:'dark',GetLanguage:'en',GetAccounts:[{id:'demo',name:'Demo',email:'demo@example.test',type:'imap',enabled:true}],GetUIState:{selectedAccountId:'unified',selectedFolderId:'inbox',selectedFolderName:'Inbox',selectedFolderType:'inbox'},GetUnifiedFolderCount:1,GetUnifiedFolderConversations:[{threadId:'thread1',accountId:'demo',folderId:'inbox',subject:'Native menu coverage',snippet:'Menu fixture',latestDate:new Date().toISOString(),messageCount:1,unreadCount:0,participants:[{name:'Fixture sender',email:'fixture@example.test'}]}],GetConversation:{id:'thread1',subject:'Native menu coverage',messages:[{id:'msg1',accountId:'demo',folderId:'inbox',threadId:'thread1',subject:'Native menu coverage',fromName:'Fixture sender',fromEmail:'fixture@example.test',toList:[],ccList:[],date:new Date().toISOString(),bodyText:'Native menus use this selected conversation.',isRead:true}]}})
      const backend=new Proxy({}, {get:(_,key)=>(...args)=>{window.backendCalls.push([key,args]);return Promise.resolve(key==='ResolveMailActionTarget'?args[0].messages.map(message=>message.messageId):key in data?data[key]:key.startsWith('Get')||key.startsWith('List')?[]:null)}})
      window.go={app:{App:backend,ComposerApp:backend,ReaderApp:{GetConfig:async()=>({accountId:'demo',folderId:'inbox',folderName:'Inbox',folderType:'inbox',threadId:'thread1',threadAccountId:'demo',threadFolderId:'inbox'}),Call:(operation,args)=>backend[operation](...args)}}}
    })
    await page.goto('http://127.0.0.1:5189',{waitUntil:'domcontentloaded'})
    await page.locator('[data-mail-row-key]').waitFor({state:'attached',timeout:5000})
    await page.mouse.move(25,300)
    await page.waitForTimeout(250)
    const enabled=id=>page.evaluate(id=>window.states.at(-1)?.enabled.includes(id),id)
    const invoke=async id=>{await page.waitForFunction(id=>window.states.at(-1)?.enabled.includes(id),id);await page.evaluate(id=>{for(const cb of window.listeners.get('native-menu:invoke')||[])cb({...window.states.at(-1),id})},id);await page.waitForTimeout(150)}
    await invoke('list.group')
    await page.getByRole('dialog').waitFor()
    await page.getByRole('dialog').getByRole('button',{name:'Dismiss',exact:true}).click()
    await page.waitForTimeout(100)
    await invoke('list.filters')
    await page.getByRole('menu').waitFor()
    await page.keyboard.press('Escape')
    await page.waitForTimeout(100)
    await invoke('nav.open')
    // Opening requires a selected item, so select via the existing row first.
    if(!await page.getByText('Native menus use this selected conversation.',{exact:true}).count())await page.locator('[data-mail-row-key]').click()
    await page.getByText('Native menus use this selected conversation.',{exact:true}).waitFor()
    await page.screenshot({path:join(captureDir,'postilaatikko-native-menu-mail-debug.png')})
    await page.waitForFunction(()=>window.states.at(-1)?.enabled.includes('mail.reply'))
    assert.ok(await enabled('mail.reply'))
    await page.locator('main[data-pane="viewer"]').click({position:{x:400,y:300}})
    await page.waitForTimeout(100)
    await invoke('mail.deletePermanently')
    await page.getByRole('alertdialog').waitFor()
    assert.equal(await page.evaluate(()=>window.backendCalls.filter(([name])=>name==='DeletePermanently').length),0,'menu deletion must stop at confirmation')
    await page.getByRole('alertdialog').getByRole('button',{name:'Cancel',exact:true}).click()
    await page.waitForTimeout(100)
    // Real inline reply uses its own Composer without App.showComposer.
    await page.evaluate(()=>{window.data.PrepareComposerReply={to:[{email:'fixture@example.test'}],subject:'Re: Native menu coverage',body:'',isHTML:true}})
    await page.locator('[data-message-id="msg1"]').focus()
    await page.waitForTimeout(100)
    await invoke('mail.reply')
    await page.locator('[data-inline-reply] [data-composer-root]').waitFor()
    await page.waitForTimeout(100)
    for(const id of ['mail.archive','mail.trash','view.calendar','view.contacts'])assert.equal(await enabled(id),false,`inline reply excludes ${id}`)
    await page.locator('[data-inline-reply] [data-native-action="app.close"]').click()
    await page.getByRole('button',{name:/Discard/i}).click()
    // Discard through the existing confirmation, not by unmounting the editor.
    await page.locator('[data-inline-reply] [data-composer-root]').waitFor({state:'detached'})
    await page.evaluate(()=>{
      const stamp=Math.floor(Date.now()/1000)
      window.data.Calendar_ListSources=[{id:'cal-source',name:'Fixture source',type:'caldav'}]
      window.data.Calendar_ListCalendars=[{id:'cal',sourceId:'cal-source',displayName:'Fixture calendar',visible:true,writable:true}]
      const event={id:'event1',calendarId:'cal',uid:'event1',summary:'Selected calendar fixture',dtstartUnix:stamp,dtendUnix:stamp+1800,instanceStartUnix:stamp,instanceEndUnix:stamp+1800,isAllDay:false}
      window.data.Calendar_ListEventsInRange=[event];window.data.Calendar_GetEvent=event
    })
    // Open a real embedded reader tab, retain its viewer focus, then hide
    // mail by switching extensions. No hidden reader may own mutations.
    await page.locator('[data-mail-row-key]').dblclick()
    await page.waitForTimeout(200)
    await page.locator('.mail-window [data-message-id="msg1"]').focus()
    await page.waitForTimeout(100)
    await invoke('view.calendar');await page.waitForTimeout(250)
    for(const id of ['mail.archive','mail.trash','mail.markRead','mail.unsnooze'])assert.equal(await enabled(id),false,`hidden embedded reader excludes ${id}`)
    assert.equal(await page.evaluate(()=>window.states.at(-1).view),'calendar')
    assert.equal(await enabled('mail.trash'),false)
    assert.equal(await enabled('calendar.today'),true)
    await page.getByText('Selected calendar fixture',{exact:true}).first().click()
    await page.locator('[role="dialog"][aria-modal="false"]').waitFor()
    await page.waitForTimeout(100)
    assert.equal(await enabled('calendar.today'),true)
    assert.equal(await enabled('calendar.focus'),true)
    assert.equal(await enabled('view.contacts'),true)
    await invoke('calendar.focus')
    await invoke('calendar.today')
    await invoke('view.contacts');await page.waitForTimeout(250)
    assert.equal(await enabled('calendar.today'),false)
    assert.equal(await enabled('contacts.new'),true)
    await page.screenshot({path:join(captureDir,'postilaatikko-native-menus-contacts.png')})
    await invoke('view.mail');await page.keyboard.press('Control+Tab');await page.waitForTimeout(100);await invoke('app.compose')
    await page.locator('[data-composer-root]').waitFor()
    await page.waitForTimeout(250)
    assert.equal(await enabled('mail.trash'),false)
    assert.equal(await enabled('composer.attach'),true)
    assert.equal(await enabled('composer.send'),false,'empty recipient list disables Send')
    await invoke('composer.identity')
    await page.getByRole('listbox').waitFor()
    await page.keyboard.press('Escape')
    await page.waitForTimeout(100)
    await page.locator('[data-composer-root] input[placeholder]:not(#composer-subject)').first().fill('recipient@example.test')
    await page.locator('[data-composer-root] input[placeholder]:not(#composer-subject)').first().press('Enter')
    await page.waitForTimeout(150)
    assert.equal(await enabled('composer.send'),true)
    await page.screenshot({path:join(captureDir,'postilaatikko-native-menus-composer.png')})
    // Empty subject opens the existing confirmation, never a backend send.
    await invoke('composer.send')
    assert.equal(await page.evaluate(()=>window.backendCalls.filter(([name])=>/^(SendMessage|SendComposed|SendMail)$/.test(name)).length),0)
    assert.deepEqual(errors,[])
    await page.addInitScript(()=>{window.addEventListener('DOMContentLoaded',()=>{window.data.GetConversation.messages.push({...window.data.GetConversation.messages[0],id:'msg2',isRead:false})})})
    await page.goto('http://127.0.0.1:5189/reader.html',{waitUntil:'domcontentloaded'})
    await page.waitForFunction(()=>window.states.at(-1)?.enabled.includes('mail.reply'))
    assert.equal(await enabled('app.compose'),false,'reader has no background primary mail command owner')
    // Mixed read/unread conversations must support both explicit states.
    await invoke('mail.markUnread')
    await page.waitForFunction(()=>window.backendCalls.some(([name,args])=>name==='MarkAsUnread'&&args[0].includes('msg2')))
    await invoke('mail.markRead')
    await page.waitForFunction(()=>window.backendCalls.some(([name,args])=>name==='MarkAsRead'&&args[0].includes('msg2')))
    await invoke('mail.deletePermanently')
    await page.getByRole('alertdialog').waitFor()
    assert.equal(await page.evaluate(()=>window.backendCalls.filter(([name])=>name==='DeletePermanently').length),0)
    await page.goto('http://127.0.0.1:5189/mail-window.html',{waitUntil:'domcontentloaded'})
    await page.waitForFunction(()=>window.states.at(-1)?.enabled.includes('nav.next'))
    assert.equal(await enabled('window.new'),true)
    await page.goto('http://127.0.0.1:5189/composer.html',{waitUntil:'domcontentloaded'})
    await page.locator('[data-composer-root]').waitFor()
    await page.waitForFunction(()=>window.states.at(-1)?.enabled.includes('composer.attach'))
    assert.equal(await enabled('mail.trash'),false)
    assert.equal(await enabled('composer.send'),false)
    await invoke('composer.toolbar')
    await page.waitForFunction(()=>window.states.at(-1)?.enabled.includes('format.bold'))
    await page.screenshot({path:join(captureDir,'postilaatikko-native-menus-detached-composer.png')})
    assert.deepEqual(errors,[])
  } finally {await browser.close();await server.close()}
})
