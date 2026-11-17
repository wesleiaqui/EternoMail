import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import ts from 'typescript'
function makeQueue(read=[],reply=[],failure=false){
  const calls=[],undos=[]
  const source=readFileSync(new URL('../src/lib/stores/later.svelte.ts',import.meta.url),'utf8').replace(/^import .*$/mg,'')
  const code=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
  const store=refs=>({refs,ready:true,load:async()=>{},has:(a,c)=>refs.some(x=>x.accountId===a&&x.conversationId===c),isBusy:()=>false})
  const change=async(a,c,on)=>{calls.push([a,c,on]);if(failure)throw Error('offline');return {changed:true,operationId:'one-op'}}
  const exports={};new Function('exports','readLaterQueue','replyLaterQueue','$state','UndoOperation','ChangeLaterWithUndo',code)(exports,store(read),store(reply),x=>x,async id=>undos.push(id),change)
  return{...exports,calls,undos}
}
const ref=(a,c)=>({accountId:a,conversationId:c})
test('one pile deduplicates both old queues and keeps account identity',()=>{const {laterQueue:q}=makeQueue([ref('a','1')],[ref('a','1'),ref('b','1')]);assert.equal(q.count,2);assert(q.has('b','1'));assert(!q.has('c','1'))})
test('new marks delegate to one atomic backend operation',async()=>{const x=makeQueue();await x.laterQueue.setWithUndo('a','1',true);assert.deepEqual(x.calls,[['a','1',true]])})
test('dual-mark removal and targeted Undo use one receipt',async()=>{const x=makeQueue([ref('a','1')],[ref('a','1')]);const result=await x.laterQueue.setWithUndo('a','1',false);assert.equal(x.calls.length,1);assert.deepEqual(result.operationIds,['one-op']);await x.undoLaterChange(result);assert.deepEqual(x.undos,['one-op'])})
test('backend failure is not converted to success or a second legacy write',async()=>{const x=makeQueue([ref('a','1')],[ref('a','1')],true);await assert.rejects(x.laterQueue.setWithUndo('a','1',false),/offline/);assert.equal(x.calls.length,1);assert.deepEqual(x.undos,[]);assert(x.laterQueue.has('a','1'))})
