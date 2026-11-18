import {test} from 'node:test'
import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {realpathSync,mkdirSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
const browserModule=process.env.POSTI_PLAYWRIGHT_PATH
const captureDir=process.env.POSTI_BROWSER_CAPTURE_DIR||join(tmpdir(),'posti-filter-captures')
mkdirSync(captureDir,{recursive:true})
test('filter editor: prefill, literal subjects, duplicate names, explicit application, reopening and settings',{skip:!browserModule,timeout:60000},async()=>{
  const root=fileURLToPath(new URL('..',import.meta.url))
  const server=await createServer({root,plugins:[{name:'filter-fixture',resolveId(id){if(id==='/filter-mount.js')return id},load(id){if(id==='/filter-mount.js')return `export {mount,unmount} from 'svelte'`}}],server:{host:'127.0.0.1',port:5193,strictPort:true,fs:{allow:[root,realpathSync(join(root,'node_modules'))]}},logLevel:'error'})
  await server.listen()
  const {chromium}=await import(browserModule)
  const browser=await chromium.launch({executablePath:process.env.POSTI_CHROMIUM_BUNDLED?undefined:'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
  try{
    const page=await browser.newPage({viewport:{width:1024,height:1100}})
    const errors=[];page.on('pageerror',error=>errors.push(error.message))
    await page.route('**/filter-fixture',route=>route.fulfill({contentType:'text/html',body:'<html data-theme="dark"><body style="background:#18191d"><div id="fixture"></div></body></html>'}))
    await page.goto('http://127.0.0.1:5193/filter-fixture')
    await page.evaluate(async()=>{
      await import('/src/app.css'); await import('/src/lib/iconify-offline.ts');window.calls=[];window.saved=[];window.existing=[{id:'older',name:'Rule for sender@example.com',enabled:true,fromContains:['older@example.com'],destFolderId:'archive'}]
      window.go={app:{App:new Proxy({},{get:(_,name)=>(...args)=>{
        window.calls.push([name,args])
        switch(name){
          case 'GetAccounts':return Promise.resolve([{id:'a',name:'Work',email:'me@example.com'}])
          case 'GetMessage':return Promise.resolve({fromEmail:'sender@example.com',toList:'[{"email":"me@example.com"}]',subject:'Hello, world'})
          case 'GetMessageListID':return Promise.resolve('news.example.com')
          case 'PreviewFilterRule':return Promise.resolve(7)
          case 'GetFolderTree':return Promise.resolve([{folder:{id:'archive',path:'Archive',name:'Archive'},children:[]}])
          case 'GetFilterRules':return Promise.resolve(window.existing)
          case 'CreateFilterRule':window.saved.push(args[0]);window.existing.push(args[0]);return Promise.resolve()
          case 'UpdateFilterRule':return Promise.resolve()
          case 'DeleteFilterRule':window.existing=window.existing.filter(rule=>rule.id!==args[0]);return Promise.resolve()
          case 'ApplyFilterRuleToExisting':return Promise.resolve(7)
          default:return Promise.resolve([])
        }
      }})}}
      await (await import('/src/lib/i18n/index.ts')).initI18n('en')
      const {mount,unmount}=await import('/filter-mount.js')
      window.show=async(settings=false,locale='en')=>{
        if(window.component)await unmount(window.component)
        await (await import('/src/lib/i18n/index.ts')).initI18n(locale)
        await new Promise(resolve=>setTimeout(resolve,100))
        const Component=(await import(settings?'/src/lib/components/settings/FiltersTab.svelte':'/src/lib/components/common/FilterDialog.svelte')).default
        window.component=mount(Component,{target:document.querySelector('#fixture'),props:settings?{}:{open:true,messageId:'message-a',accountId:'a'}})
      }
      await window.show()
    })
    const dialog=page.getByRole('dialog')
    await page.getByLabel('From',{exact:true}).first().waitFor()
    await page.waitForTimeout(600)
    assert.equal(await page.getByLabel('From',{exact:true}).first().isChecked(),true)
    assert.equal(await page.getByLabel('List-ID',{exact:true}).first().isChecked(),false)
    const existing=page.locator('input[type=checkbox]').last()
    assert.equal(await existing.isChecked(),false)
    await page.getByLabel('Move to folder',{exact:true}).uncheck()
    await page.getByText('Mark read',{exact:true}).click()
    await page.getByLabel('Subject',{exact:true}).first().check()
    await page.waitForTimeout(500)
    await page.screenshot({path:join(captureDir,'filter-create-en.png'),fullPage:true})
    await dialog.getByRole('button',{name:'Create filter',exact:true}).click()
    await page.waitForTimeout(60)
    assert.equal(await page.evaluate(()=>window.calls.filter(([name])=>name==='ApplyFilterRuleToExisting').length),0)
    const saved=await page.evaluate(()=>window.saved.at(-1))
    assert.deepEqual(saved.subjectContains,['Hello, world'])
    assert.ok(saved.id&&saved.id!=='older')
    await page.evaluate(()=>window.show())
    await page.waitForTimeout(550)
    assert.equal(await page.locator('input[type=checkbox]').last().isChecked(),false)
    await page.getByLabel('Move to folder',{exact:true}).uncheck()
    await page.getByText('Mark read',{exact:true}).click()
    await page.locator('input[type=checkbox]').last().check()
    await dialog.getByRole('button',{name:'Create filter',exact:true}).click()
    await page.waitForTimeout(70)
    assert.equal(await page.evaluate(()=>window.calls.filter(([name])=>name==='ApplyFilterRuleToExisting').at(-1)[1][0]),await page.evaluate(()=>window.saved.at(-1).id),'application uses the new UUID, never an older duplicate name')
    await page.evaluate(async()=>{
      const {mount,unmount}=await import('/filter-mount.js')
      await unmount(window.component)
      const Component=(await import('/tests/fixtures/FilterContextMenu.svelte')).default
      window.component=mount(Component,{target:document.querySelector('#fixture')})
    })
    await page.getByRole('button',{name:'Selected message'}).click({button:'right'})
    await page.getByRole('menuitem',{name:'Create filter...'}).waitFor()
    await page.screenshot({path:join(captureDir,'filter-context-menu.png'),fullPage:true})
    await page.getByRole('menuitem',{name:'Create filter...'}).click()
    await page.locator('#filter-name').waitFor()
    await page.getByRole('button',{name:'Cancel',exact:true}).click()
    await page.evaluate(()=>window.show(true))
    await page.getByText('Rule for sender@example.com',{exact:true}).first().waitFor()
    await page.screenshot({path:join(captureDir,'filter-settings-en.png'),fullPage:true})
    await page.getByTitle('Move down').first().click()
    await page.waitForTimeout(80)
    await page.getByRole('switch').first().click()
    await page.waitForTimeout(80)
    assert.equal(await page.evaluate(()=>window.calls.filter(([name])=>name==='UpdateFilterRule').at(-1)[1][0].orderIndex),0,'toggle keeps reordered priority')
    await page.getByTitle('Edit filter').first().click()
    await page.getByRole('dialog').waitFor()
    await page.getByRole('button',{name:'Cancel',exact:true}).click()
    await page.getByRole('button',{name:'New filter',exact:true}).click()
    await page.waitForTimeout(100)
    assert.equal(await page.locator('#filter-name').inputValue(),'','edit state does not leak into creation')
    await page.evaluate(()=>window.show(false,'fi'))
    await page.waitForTimeout(600)
    await page.setViewportSize({width:680,height:950})
    await page.screenshot({path:join(captureDir,'filter-create-fi.png'),fullPage:true})
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'no horizontal overflow')
    assert.deepEqual(errors,[])
  }finally{await browser.close();await server.close()}
})
