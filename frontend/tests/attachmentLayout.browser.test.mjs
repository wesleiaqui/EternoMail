import {test} from 'node:test'
import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {realpathSync,mkdirSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
const browserModule=process.env.POSTI_PLAYWRIGHT_PATH
const captureDir=process.env.POSTI_BROWSER_CAPTURE_DIR||join(tmpdir(),'posti-attachment-captures')
mkdirSync(captureDir,{recursive:true})
test('real attachment component: counts, chips, one line, expansion, routing and encrypted multiplicity',{skip:!browserModule,timeout:60000},async()=>{
  const root=fileURLToPath(new URL('..',import.meta.url))
  const server=await createServer({root,plugins:[{name:'attachment-fixture',resolveId(id){if(id==='/attachment-mount.js')return id},load(id){if(id==='/attachment-mount.js')return `export {mount,unmount} from 'svelte'`}}],server:{host:'127.0.0.1',port:5192,strictPort:true,fs:{allow:[root,realpathSync(join(root,'node_modules'))]}},logLevel:'error'})
  await server.listen()
  const {chromium}=await import(browserModule)
  const browser=await chromium.launch({executablePath:process.env.POSTI_CHROMIUM_BUNDLED?undefined:'/usr/bin/google-chrome-stable',headless:true,args:['--no-sandbox']})
  try {
    const page=await browser.newPage({viewport:{width:850,height:650}})
    const errors=[];page.on('pageerror',error=>errors.push(error.message))
    await page.route('**/attachment-fixture',route=>route.fulfill({contentType:'text/html',body:'<html class="dark" data-theme="dark"><body style="margin:24px;background:#18191d;color:white"><h1 style="font:20px sans-serif">Message attachments</h1><div id="fixture"></div></body></html>'}))
    await page.goto('http://127.0.0.1:5192/attachment-fixture')
    await page.evaluate(async()=>{
      await import('/src/app.css')
      window.calls=[];window.attachments=[];window.cardToggles=0
      window.go={app:{App:new Proxy({},{get:(_,name)=>(...args)=>{window.calls.push([name,args]);return Promise.resolve(name==='GetMessageAttachments'?window.attachments:'')}})}}
      await (await import('/src/lib/i18n/index.ts')).initI18n('en')
      const {mount,unmount}=await import('/attachment-mount.js')
      const {default:Component}=await import('/tests/fixtures/AttachmentReaderCard.svelte')
      window.show=async(count,encrypted=false)=>{
        if(window.component)await unmount(window.component)
        window.attachments=Array.from({length:count},(_,i)=>({id:`part-${i}`,filename:i<2?'same-name-report.pdf':`Very-long-original-file-name-${i}.pdf`,contentType:'application/pdf',size:(i+1)*1024,isInline:false}))
        window.component=mount(Component,{target:document.querySelector('#fixture'),props:{accountId:'account-A',messageId:'message-A',...(encrypted?{encryptedAttachments:window.attachments}:{})}})
      }
    })
    for(const count of [0,1,3,4,19]){
      await page.evaluate(count=>window.show(count),count);await page.waitForTimeout(50)
      assert.equal(await page.locator('[data-attachment-count]').count(),count?1:0)
      if(!count)continue
      assert.equal(await page.locator('.attachment-chip').count(),count<=3?count:0)
      const section=page.locator('.attachment-set')
      assert.equal(await section.getAttribute('data-attachment-expanded'),'false')
      const collapsed=await section.boundingBox()
      assert.ok(collapsed.height<55,`count ${count} remains a single line`)
      if(count===3)await page.screenshot({path:join(captureDir,'postilaatikko-attachment-chips.png')})
      if(count===19)await page.screenshot({path:join(captureDir,'postilaatikko-attachment-bar.png')})
      await page.getByRole('button',{name:'Expand attachments',exact:true}).focus()
      await page.keyboard.press('Enter')
      await section.locator('li').first().waitFor()
      assert.equal(await section.locator('li').count(),count)
      if(count===19)await page.screenshot({path:join(captureDir,'postilaatikko-attachments-expanded.png'),fullPage:true})
      await section.locator('li').last().getByRole('button',{name:/^Download:/}).click()
      assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SaveAttachmentAsForAccount').at(-1)),['SaveAttachmentAsForAccount',['account-A','message-A',`part-${count-1}`]])
      await page.getByRole('button',{name:'Collapse attachments',exact:true}).focus()
      await page.keyboard.press('Space')
      if(count>1){await page.getByRole('button',{name:/Save All/}).click();assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SaveAllAttachmentsForAccount').at(-1)),['SaveAllAttachmentsForAccount',['account-A','message-A']])}
    }
    await page.evaluate(()=>window.show(3))
    await page.setViewportSize({width:360,height:650});await page.waitForTimeout(60)
    assert.equal(await page.locator('.attachment-chip').count(),3)
    assert.ok((await page.locator('.attachment-line').boundingBox()).height<65)
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'narrow layout does not overflow horizontally')
    await page.screenshot({path:join(captureDir,'postilaatikko-attachment-chips-narrow.png')})
    await page.locator('.attachment-chip').nth(1).getByRole('button',{name:/^Open:/}).click()
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='OpenAttachmentForAccount').at(-1)),['OpenAttachmentForAccount',['account-A','message-A','part-1']])
    await page.evaluate(()=>window.show(4,true));await page.waitForTimeout(50)
    await page.getByRole('button',{name:'Expand attachments',exact:true}).click()
    assert.equal(await page.locator('li').count(),4,'encrypted duplicates remain distinct list entries')
    await page.locator('li').last().getByRole('button',{name:/^Download:/}).click()
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SaveEncryptedAttachmentAs').at(-1)),['SaveEncryptedAttachmentAs',['message-A','Very-long-original-file-name-3.pdf']])
    await page.getByRole('button',{name:/Save All/}).click()
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(([name])=>name==='SaveAllEncryptedAttachments').at(-1)),['SaveAllEncryptedAttachments',['message-A']])
    assert.equal(await page.evaluate(()=>window.cardToggles),0,'reader card never steals attachment keyboard activation')
    assert.deepEqual(errors,[])
  }finally{await browser.close();await server.close()}
})
