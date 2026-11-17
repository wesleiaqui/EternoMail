import {test} from 'node:test'
import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {fileURLToPath} from 'node:url'
import {mkdirSync} from 'node:fs'
const browserModule=process.env.POSTI_PLAYWRIGHT_PATH
const captureDir=process.env.POSTI_BROWSER_CAPTURE_DIR||'/tmp/posti-search-captures'
mkdirSync(captureDir,{recursive:true})
test('real search workspace: debounce, supersession, all-account row routing, scope, close and explicit save',{skip:!browserModule,timeout:90000},async()=>{
  const root=fileURLToPath(new URL('..',import.meta.url))
  const server=await createServer({root,plugins:[{name:'search-fixture',resolveId(id){if(id==='/search-mount.js')return id},load(id){if(id==='/search-mount.js')return `export {mount} from 'svelte'`}}],server:{host:'127.0.0.1',port:5193,strictPort:true},logLevel:'error'})
  await server.listen()
  const {chromium}=await import(browserModule)
  const browser=await chromium.launch({executablePath:process.env.POSTI_CHROMIUM_BUNDLED?undefined:'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
  try{
    const page=await browser.newPage({viewport:{width:1280,height:900}})
    const timing=[]
    page.on('console',message=>{if(message.text().startsWith('[search timing]'))timing.push(message.text())})
    const errors=[];page.on('pageerror',e=>errors.push(e.message))
    await page.route('**/search-fixture',r=>r.fulfill({contentType:'text/html',body:'<html class="dark" data-theme="dark"><body style="margin:0"><div id="fixture"></div></body></html>'}))
    await page.goto('http://127.0.0.1:5193/search-fixture')
    await page.evaluate(async()=>{
      await import('/src/app.css')
      window.calls=[]
      const row=(query,accountId='b')=>({threadId:`${accountId}-${query}`,subject:`Result: ${query}`,accountId,folderId:`folder-${accountId}`,folderName:'Archive',folderType:'archive',accountName:`Account ${accountId}`,participants:[{name:'Test Sender',email:'sender@example.test'}],snippet:'Synthetic search result',latestDate:'2026-11-17T10:00:00Z',messageIds:[`message-${accountId}`],messageCount:1,unreadCount:0})
      window.runtime=new Proxy({EventsOnMultiple:()=>()=>{}},{get:(target,key)=>target[key]||(()=>Promise.resolve())})
      window.go={app:{App:new Proxy({},{get:(_,name)=>(...args)=>{
        window.calls.push({name,args,at:performance.now()})
        if(name==='SearchMail'){
          const req=args[0],delay=req.query==='slow'?750:35
          return new Promise(resolve=>setTimeout(()=>resolve({conversations:[row(req.query,req.accountIds?.[0]||'b')],cursor:'',issues:[]}),delay))
        }
        if(name==='GetFTSIndexStatus')return Promise.resolve({complete:true,totalMessages:0,indexedMessages:0})
        if(name==='IsFTSIndexing')return Promise.resolve(false)
        if(/Count/.test(name))return Promise.resolve(0)
        if(name==='CreateVirtualFolder')return Promise.resolve()
        return Promise.resolve([])
      }})}}
      await (await import('/src/lib/i18n/index.ts')).initI18n('en')
      const {accountStore}=await import('/src/lib/stores/accounts.svelte.ts')
      accountStore.accounts=['a','b'].map(id=>({account:{id,name:`Account ${id}`,email:`${id}@example.test`,enabled:true,authType:'oauth2',imapHost:'imap.gmail.com'},folders:[],loading:false,syncing:false,error:null,lastSync:null}))
      const {mount}=await import('/search-mount.js')
      const {default:Component}=await import('/tests/fixtures/SearchWorkspace.svelte')
      window.component=mount(Component,{target:document.querySelector('#fixture')})
    })
    await page.getByRole('button',{name:'Search all mail',exact:true}).click()
    const search=page.getByRole('searchbox',{name:'Search all mail',exact:true})
    await search.fill('slow');await page.waitForTimeout(300)
    await search.fill('meeting')
    await page.getByText('Result: meeting',{exact:true}).waitFor()
    await page.waitForTimeout(550)
    assert.equal(await page.getByText('Result: slow',{exact:true}).count(),0,'late provider response cannot overwrite a newer query')
    await page.screenshot({path:`${captureDir}/search-modal.png`})
    await search.press('Enter')
    await page.getByRole('heading',{name:'Search: meeting',exact:true}).waitFor()
    const result=page.getByText('Result: meeting',{exact:true})
    await result.waitFor()
    await page.screenshot({path:`${captureDir}/search-results.png`})
    await result.click()
    await page.getByText('b:folder-b:b-meeting',{exact:true}).waitFor()
    assert.equal(await page.evaluate(()=>window.calls.filter(c=>c.name==='CreateVirtualFolder').length),0,'temporary results never save a folder')
    await page.getByRole('button',{name:'Unread fixture filter',exact:true}).dispatchEvent('click')
    await page.waitForTimeout(100)
    assert.equal(await page.evaluate(()=>window.calls.filter(c=>c.name==='SearchMail').at(-1).args[0].filter),'unread','global quick filter reaches provider request')
    assert.equal(await page.getByRole('button',{name:'Save',exact:true}).isDisabled(),true,'save cannot silently omit quick filter')
    await page.getByRole('button',{name:'Clear fixture filter',exact:true}).dispatchEvent('click')
    await page.getByRole('button',{name:'Edit',exact:true}).click()
    await page.getByLabel('Scope',{exact:true}).selectOption('a')
    await page.waitForTimeout(400)
    const request=await page.evaluate(()=>window.calls.filter(c=>c.name==='SearchMail').at(-1).args[0])
    assert.deepEqual(request.accountIds,['a'])
    await page.getByRole('searchbox',{name:'Search messages',exact:true}).fill('slow')
    await page.waitForTimeout(350)
    assert.equal(await page.getByText('Result: meeting',{exact:true}).count(),1,'previous rows remain visible during refinement')
    await page.getByRole('searchbox',{name:'Search messages',exact:true}).fill('meeting')
    await page.getByLabel('Scope',{exact:true}).selectOption('')
    await page.waitForTimeout(400)
    await page.screenshot({path:`${captureDir}/search-edit.png`})
    await page.getByRole('searchbox',{name:'Search messages',exact:true}).fill('')
    assert.equal(await page.locator('[data-mail-row-key]').count(),0,'empty global query never reveals underlying folder rows')
    assert.equal(await page.getByRole('heading',{name:'Search:',exact:true}).count(),1)
    await page.getByRole('searchbox',{name:'Search messages',exact:true}).fill('meeting')
    await page.getByText('Result: meeting',{exact:true}).waitFor()
    await page.getByRole('button',{name:'Save',exact:true}).click()
    await page.getByRole('dialog').waitFor()
    assert.equal(await page.evaluate(()=>window.calls.filter(c=>c.name==='CreateVirtualFolder').length),0,'opening save dialog is not a write')
    await page.screenshot({path:`${captureDir}/search-save.png`})
    await page.getByRole('dialog').getByRole('button',{name:'Save',exact:true}).click()
    const saved=await page.evaluate(()=>window.calls.filter(c=>c.name==='CreateVirtualFolder').map(c=>c.args[0]))
    assert.equal(saved.length,1)
    assert.equal(saved[0].searchQuery,'meeting')
    assert.equal(saved[0].folderMode,'exclude')
    assert.deepEqual(saved[0].folderIds,[])
    await page.getByRole('button',{name:'Close search',exact:true}).click()
    assert.equal(await page.getByText('Temporary search',{exact:true}).count(),0)
    const calls=await page.evaluate(()=>window.calls.filter(c=>c.name==='SearchMail'))
    console.log('Synthetic SearchMail calls:',JSON.stringify(calls.map(c=>({query:c.args[0].query,at:c.at}))))
    await page.addInitScript(() => {
      if(window.top!==window)return
      localStorage.setItem('postilaatikko:context-pane-auto-hide','false')
      window.runtime=new Proxy({EventsOnMultiple:()=>()=>{}},{get:(obj,key)=>obj[key]||(()=>Promise.resolve())})
      const stamp=new Date().toISOString()
      const data={
        IsReady:true,GetThemeMode:'dark',GetLanguage:'en',
        GetAccounts:[{id:'demo',name:'Demo',email:'demo@example.test',type:'imap',enabled:true}],
        GetUIState:{selectedAccountId:'unified',selectedFolderId:'inbox',selectedFolderName:'Inbox',selectedFolderType:'inbox'},
        GetUnifiedFolderCount:1,
        GetUnifiedFolderConversations:[{threadId:'thread1',accountId:'demo',folderId:'inbox',subject:'Search shortcut test',snippet:'Open reader',latestDate:stamp,messageCount:1,unreadCount:0,participants:[{name:'Fixture sender',email:'fixture@example.test'}]}],
        GetConversation:{id:'thread1',subject:'Search shortcut test',messages:[{id:'msg1',accountId:'demo',folderId:'inbox',threadId:'thread1',subject:'Search shortcut test',fromName:'Fixture sender',fromEmail:'fixture@example.test',toList:[],ccList:[],date:stamp,bodyText:'Find this literal meeting in the reader.',isRead:true},{id:'msg2',accountId:'demo',folderId:'inbox',threadId:'thread1',subject:'HTML search test',fromName:'Fixture sender',fromEmail:'fixture@example.test',toList:[],ccList:[],date:stamp,bodyHtml:'<p>HTML meeting content</p>',isRead:true}]},
        SearchMail:{conversations:[],cursor:'',issues:[]},
      }
      window.go={app:{App:new Proxy({},{get:(_,key)=>()=>Promise.resolve(key in data?data[key]:key.startsWith('Get')||key.startsWith('List')?[]:null)})}}
    })
    await page.goto('http://127.0.0.1:5193',{waitUntil:'domcontentloaded'})
    await page.locator('[data-mail-row-key]').click()
    await page.getByText('Find this literal meeting in the reader.',{exact:true}).waitFor()
    await page.frameLocator('iframe').locator('body').click()
    await page.keyboard.press('Control+f')
    await page.getByRole('searchbox',{name:'Find in message',exact:true}).fill('meeting')
    await page.locator('mark[data-reader-find]').waitFor()
    await page.frameLocator('iframe').locator('mark[data-reader-find]').waitFor()
    await page.screenshot({path:`${captureDir}/search-reader-find.png`})
    await page.keyboard.press('Control+f')
    await page.getByRole('dialog').waitFor()
    assert.equal(await page.getByRole('searchbox',{name:'Search all mail',exact:true}).inputValue(),'meeting')
    await page.screenshot({path:`${captureDir}/search-main-modal.png`})
    await page.keyboard.press('Escape')
    await page.keyboard.press('Control+Shift+f')
    await page.getByRole('dialog').waitFor()
    await page.keyboard.press('Escape')
    await page.setViewportSize({width:700,height:900})
    await page.locator('[data-mail-row-key]').click()
    await page.keyboard.press('Control+Shift+f')
    await page.getByRole('searchbox',{name:'Search all mail',exact:true}).fill('narrow')
    await page.keyboard.press('Enter')
    await page.getByRole('heading',{name:'Search: narrow',exact:true}).waitFor()
    await page.waitForFunction(async()=> (await import('/src/lib/stores/layout.svelte.ts')).getResponsiveView()==='default')
    await page.setViewportSize({width:1280,height:900})
    await page.goto('http://127.0.0.1:5193/search-fixture')
    await page.evaluate(async()=>{
      await import('/src/app.css')
      await (await import('/src/lib/i18n/index.ts')).initI18n('en')
      const {messageFind}=await import('/src/lib/components/viewer/messageFind.svelte.ts')
      messageFind.open=false;messageFind.query=''
      const {mount}=await import('/search-mount.js')
      const {default:Window}=await import('/src/MailWindowApp.svelte')
      mount(Window,{target:document.querySelector('#fixture'),props:{readerTab:true,config:{accountId:'demo',folderId:'inbox',folderName:'Inbox',folderType:'inbox',threadId:'thread1'}}})
    })
    await page.getByText('Find this literal meeting in the reader.',{exact:true}).waitFor()
    await page.frameLocator('iframe').locator('body').click()
    await page.keyboard.press('Control+f')
    await page.getByRole('searchbox',{name:'Find in message',exact:true}).fill('meeting')
    await page.locator('mark[data-reader-find]').waitFor()
    await page.frameLocator('iframe').locator('mark[data-reader-find]').waitFor()
    await page.keyboard.press('Control+f')
    await page.getByRole('dialog').waitFor()
    assert.equal(await page.getByRole('searchbox',{name:'Search all mail',exact:true}).inputValue(),'meeting')
    await page.keyboard.press('Enter')
    await page.getByRole('heading',{name:'Search: meeting',exact:true}).waitFor()
    await page.screenshot({path:`${captureDir}/search-detached-results.png`})
    console.log('Development first-result timing (synthetic provider):',JSON.stringify(timing))
    assert.deepEqual(errors,[])
  }finally{await browser.close();await server.close()}
})
