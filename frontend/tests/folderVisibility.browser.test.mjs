import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { mkdirSync, realpathSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
const browserModule = process.env.POSTI_PLAYWRIGHT_PATH
const captures = process.env.POSTI_BROWSER_CAPTURE_DIR || '/tmp/hide-captures'
mkdirSync(captures, { recursive: true })

test('account folder controls preserve selections and references, confirm descendants and report failed catch-up', { skip: !browserModule, timeout: 90000 }, async () => {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const server = await createServer({root, plugins:[{name:'hide-fixture',resolveId(id){if(id==='/hide-mount.js')return id},load(id){if(id==='/hide-mount.js')return `export {mount,unmount} from 'svelte'`}}],server:{host:'127.0.0.1',port:5198,strictPort:true,fs:{allow:[root,realpathSync(`${root}/node_modules`)]}},logLevel:'error'})
  await server.listen()
  const {chromium} = await import(browserModule)
  const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
  try {
    const page=await browser.newPage({viewport:{width:880,height:800}})
    const errors=[];page.on('pageerror',err=>errors.push(err.message))
    await page.route('**/hide-fixture',route=>route.fulfill({contentType:'text/html',body:'<html class="dark" data-theme="dark"><body style="background:#18191d;color:white;padding:30px"><div id="fixture"></div></body></html>'}))
    await page.goto('http://127.0.0.1:5198/hide-fixture')
    await page.evaluate(async()=>{
      await import('/src/app.css')
      window.calls=[]
      window.folders=[
        {id:'inbox',accountId:'a',name:'Inbox',path:'Inbox',type:'inbox',hidden:false},
        {id:'parent',accountId:'a',name:'Old projects',path:'Old projects',type:'folder',hidden:false},
        {id:'child',accountId:'a',name:'2016',path:'Old projects/2016',parentId:'parent',type:'folder',hidden:false},
        {id:'archive',accountId:'a',name:'Archive 2015',path:'Archive 2015',type:'folder',hidden:true},
      ]
      window.views=[{id:'saved',name:'Project correspondence',folderIds:['parent'],folderMode:'include'}]
      window.rules=[{id:'rule',name:'Invoices to projects',destFolderId:'parent',enabled:true}]
      window.go={app:{App:new Proxy({},{get:(_,name)=>(...args)=>{
        window.calls.push([name,args])
        if(name==='GetFolders')return Promise.resolve(structuredClone(window.folders))
        if(name==='GetVirtualFolders')return Promise.resolve(window.views)
        if(name==='GetFilterRules')return Promise.resolve(window.rules)
        if(name==='SetFolderVisibility'){
          for(const folder of window.folders)if(args[1].includes(folder.id))folder.hidden=args[2]
          if(window.failRefresh&&args[3])return Promise.reject('Folder shown, but catch-up failed')
        }
        return Promise.resolve(null)
      }})}}
      await(await import('/src/lib/i18n/index.ts')).initI18n('en')
      const {mount}=await import('/hide-mount.js')
      const {default:Component}=await import('/src/lib/components/settings/account/AccountFoldersTab.svelte')
      window.component=mount(Component,{target:document.querySelector('#fixture'),props:{accountId:'a'}})
    })
    await page.getByText('Old projects',{exact:true}).waitFor()
    await page.locator('[data-folder-row="parent"]').getByRole('button',{name:'Hide',exact:true}).click()
    await page.getByRole('dialog').waitFor()
    assert.equal(await page.evaluate(async()=>(await import('/src/lib/stores/dialogGuard.ts')).isDialogGuardActive()),true)
    await page.getByRole('button',{name:'Cancel',exact:true}).focus()
    await page.keyboard.press('Delete')
    assert.equal(await page.getByRole('dialog').count(),1)
    await page.getByText(/Stale saved views/).waitFor()
    await page.getByText(/Filters paused/).waitFor()
    await page.screenshot({path:`${captures}/hide-folder-confirmation.png`})
    await page.getByRole('checkbox',{name:'Also hide 1 subfolders'}).check()
    await page.getByRole('dialog').getByRole('button',{name:'Hide from Postilaatikko',exact:true}).click()
    await page.getByRole('dialog').waitFor({state:'hidden'})
    assert.equal(await page.evaluate(async()=>(await import('/src/lib/stores/dialogGuard.ts')).isDialogGuardActive()),false)
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SetFolderVisibility').at(-1)),['SetFolderVisibility',['a',['parent','child'],true,false]])
    assert.equal(await page.evaluate(()=>window.rules[0].enabled),true,'hiding must not rewrite filter intent')
    assert.deepEqual(await page.evaluate(()=>window.views[0].folderIds),['parent'],'virtual links survive')
    await page.getByRole('combobox',{name:'Show folders'}).selectOption('hidden')
    await page.waitForFunction(() => !document.querySelector('[aria-busy="true"]'))
    await page.screenshot({path:`${captures}/account-hidden-folders.png`})
    assert.equal(await page.locator('[data-folder-row="inbox"]').count(),0)
    await page.getByRole('checkbox',{name:'Select Old projects',exact:true}).check()
    await page.getByRole('textbox',{name:'Search folders...'}).fill('2015')
    await page.getByRole('button',{name:'Show selected',exact:true}).click()
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SetFolderVisibility').at(-1)),['SetFolderVisibility',['a',['parent'],false,false]])
    await page.getByRole('textbox',{name:'Search folders...'}).fill('')
    await page.evaluate(()=>window.failRefresh=true)
    await page.locator('[data-folder-row="archive"]').getByRole('button',{name:'Show + refresh'}).click()
    await page.getByRole('alert').filter({hasText:'catch-up failed'}).waitFor()
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SetFolderVisibility').at(-1)),['SetFolderVisibility',['a',['archive'],false,true]])
    const placement=await page.evaluate(async()=>{
      const {accountStore}=await import('/src/lib/stores/accounts.svelte.ts')
      const {navigationCards:nav}=await import('/src/lib/stores/navigationCards.svelte.ts')
      const {getUIState}=await import('/src/lib/stores/uiState.svelte.ts')
      const entries=[{id:'folder:a:parent',kind:'folder',accountId:'a',folderId:'parent',name:'Old'},{id:'other',kind:'system',name:'Other'}]
      nav.sync(entries)
      nav.update({...nav.state,cards:[...nav.state.cards,{id:'custom',name:'Custom',hidden:false,collapsed:false,entries:[]}]})
      nav.moveEntry('folder:a:parent','custom',0);nav.update({...nav.state,quick:['folder:a:parent']})
      accountStore.folderCatalog={a:[{id:'parent',hidden:true}]}
      nav.sync(entries)
      const invisible=!nav.entries.some(e=>e.folderId==='parent')&&!nav.quick.some(e=>e.folderId==='parent')
      nav.toggleQuick('other')
      const saved=JSON.parse(getUIState().sidebarCards)
      accountStore.folderCatalog={a:[{id:'parent',hidden:false}]};nav.sync(entries)
      return {invisible,card:saved.cards.find(c=>c.id==='custom').entries,quick:nav.quick.map(e=>e.id)}
    })
    assert.equal(placement.invisible,true)
    assert.deepEqual(placement.card,['folder:a:parent'])
    assert.ok(placement.quick.includes('folder:a:parent'))
    assert.equal(await page.evaluate(async()=>{
      const {accountStore}=await import('/src/lib/stores/accounts.svelte.ts')
      window.folders.forEach(f=>f.hidden=true)
      accountStore.accounts=[{account:{id:'a',email:'a@example.org'},folders:[],syncing:false,loading:false,error:null}]
      await accountStore.syncAllComplete()
      return accountStore.accounts[0].syncing
    }),false,'all-hidden sync must settle without folder events')
    assert.deepEqual(errors,[])
    await page.setViewportSize({width:390,height:800})
    await page.screenshot({path:`${captures}/account-folders-narrow.png`,fullPage:true})
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
  } finally { await browser.close();await server.close() }
})
