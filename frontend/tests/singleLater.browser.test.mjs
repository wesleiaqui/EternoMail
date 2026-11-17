import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawn} from 'node:child_process'
import {setTimeout as delay} from 'node:timers/promises'
import {fileURLToPath} from 'node:url'
const browserModule=process.env.POSTI_PLAYWRIGHT_PATH

test('single Later pile preserves old marks and explicit removal in real App', {skip:!browserModule,timeout:60000},async()=>{
  const {chromium}=await import(browserModule)
  const server=spawn(process.execPath,['node_modules/vite/bin/vite.js','--host','127.0.0.1','--port','5189','--strictPort'],{cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']})
  let browser,log=''
  server.stdout.on('data',d=>{log+=d});server.stderr.on('data',d=>{log+=d})
  try{
    for(let i=0;i<100;i++){try{if((await fetch('http://127.0.0.1:5189')).ok)break}catch{ /* Vite is still starting. */ }if(i===99)throw Error(log);await delay(100)}
    browser=await chromium.launch({executablePath:'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
    const page=await browser.newPage({viewport:{width:1200,height:800}})
    const errors=[];page.on('pageerror',e=>errors.push(e.message))
    await page.addInitScript(()=>{
      localStorage.setItem('postilaatikko:context-pane-auto-hide','false')
      const listeners={},read=[{accountId:'demo',conversationId:'thread1'}],reply=[{accountId:'demo',conversationId:'thread1'}],calls=[]
      window.fixture={read,reply,calls}
      window.runtime=new Proxy({}, {get:(_,key)=>key==='EventsOnMultiple'?(name,cb)=>{(listeners[name]||=[]).push(cb);return()=>{}}:()=>Promise.resolve()})
      const emit=(name,payload)=>(listeners[name]||[]).forEach(cb=>cb(payload))
      const item={threadId:'thread1',accountId:'demo',folderId:'inbox',subject:'Projektin aikataulu',snippet:'Sopisiko ensi viikon tiistai?',latestDate:new Date().toISOString(),messageCount:1,unreadCount:0,participants:[{name:'Esimerkkilähettäjä',email:'fixture@example.test'}]}
      const data={IsReady:true,GetThemeMode:'dark',GetLanguage:'fi',GetAccounts:[{id:'demo',name:'Esimerkkitili',email:'demo@example.test',type:'imap',enabled:true}],GetUIState:{selectedAccountId:'unified',selectedFolderId:'inbox',selectedFolderName:'Saapuneet',selectedFolderType:'inbox'},GetUnifiedFolderCount:1,GetUnifiedFolderConversations:[item],GetConversation:{id:'thread1',subject:item.subject,messages:[{id:'msg1',accountId:'demo',folderId:'inbox',threadId:'thread1',subject:item.subject,fromName:'Esimerkkilähettäjä',fromEmail:'fixture@example.test',toList:[],ccList:[],date:new Date().toISOString(),bodyText:'Sopisiko ensi viikon tiistai projektin aloitukselle?',isRead:true}]}}
      window.go={app:{App:new Proxy({}, {get:(_,key)=>(...args)=>{
        calls.push([key,...args])
        if(key==='GetReadLaterQueue')return Promise.resolve([...read]);if(key==='GetReplyLaterQueue')return Promise.resolve([...reply])
        if(key==='ChangeLaterWithUndo'){
          const [accountId,conversationId,on]=args
          for(const target of [read,reply]){const i=target.findIndex(x=>x.accountId===accountId&&x.conversationId===conversationId);if(i>=0)target.splice(i,1)}
          if(on)read.push({accountId,conversationId})
          emit('readLater:refresh',{});emit('replyLater:refresh',{})
          return Promise.resolve({changed:true,operationId:'later-op'})
        }
        return Promise.resolve(key in data?data[key]:key.startsWith('Get')||key.startsWith('List')?[]:null)
      }})}}
    })
    await page.goto('http://127.0.0.1:5189',{waitUntil:'domcontentloaded'})
    await page.locator('[data-mail-row-key]').first().click()
    await page.getByText('Sopisiko ensi viikon tiistai projektin aloitukselle?',{exact:true}).waitFor()
    const button=page.locator('main[data-pane="viewer"] button[aria-label="Poista myöhemmistä"]').first()
    await button.waitFor()
    await page.waitForFunction(() => { const b = document.querySelector('main[data-pane="viewer"] button[aria-label="Poista myöhemmistä"]'); return b && !b.disabled })
    await page.mouse.move(1150,700)
    await page.waitForTimeout(1000)
    assert.equal(await button.getAttribute('aria-pressed'),'true')
    assert.equal((await page.evaluate(()=>window.fixture.read.length+window.fixture.reply.length)),2,'reading did not remove either legacy mark')
    assert.equal(await page.getByRole('button',{name:'Lue myöhemmin',exact:true}).count(),0)
    assert.equal(await page.getByRole('button',{name:'Vastaa myöhemmin',exact:true}).count(),0)
    await page.screenshot({path:'/downloads/posti-later-implemented.png',fullPage:true})
    await button.click()
    await page.waitForFunction(()=>window.fixture.read.length===0&&window.fixture.reply.length===0)
    assert.equal((await page.evaluate(()=>window.fixture.calls.filter(x=>x[0].startsWith('Change')&&x[0].includes('Later')).length)),1)
    await page.screenshot({path:'/downloads/posti-later-removed.png',fullPage:true})
    assert.deepEqual(errors,[])
    await page.close()
  }finally{if(browser)await browser.close();server.kill('SIGTERM')}
})
