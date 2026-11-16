// Native clicks call the very same UI handlers as buttons/shortcuts. There is
// no backend mutation RPC here and no synthetic keyboard-event dispatch.
import catalog from '../../../internal/nativemenu/catalog.json'
import { insertClipboardTextAtFocus } from './nativePaste'
import { ClipboardGetText, EventsEmit, EventsOn } from '../../wailsjs/runtime/runtime'

type Definition = { label: string; id?: string; children?: Definition[] }
type Action = { run: () => unknown; available: () => boolean; token?: string }
const ids = new Set<string>()
function collect(items: Definition[]) { for (const item of items) { if (item.id) ids.add(item.id); if (item.children) collect(item.children) } }
collect(catalog)
const actions = new Map<string, Set<Action>>()
const identities = new WeakMap<object, number>()
let nextIdentity = 0
function identity(value: object) { let id = identities.get(value); if (!id) { id = ++nextIdentity; identities.set(value, id) }; return id }
let invalidate = () => {}

/** Register a view-local action; remove it when its owner unmounts. */
export function registerMenuAction(id: string, run: () => unknown, available: () => boolean = () => true): () => void {
  if (!ids.has(id)) throw new Error(`Unknown native menu action: ${id}`)
  const entry = { run, available }
  const list = actions.get(id) ?? new Set<Action>()
  list.add(entry); actions.set(id, list); invalidate()
  return () => { list.delete(entry); if (!list.size) actions.delete(id); invalidate() }
}

function visible(element: HTMLElement) {
  return element.isConnected && element.getClientRects().length > 0 &&
    !element.closest('[hidden], [inert], [aria-hidden="true"]') &&
    !element.matches(':disabled, [aria-disabled="true"]')
}

function readScope(root: Document) {
  const composer = [...root.querySelectorAll<HTMLElement>('[data-composer-root]')].find(visible)
  const modal = [...root.querySelectorAll<HTMLElement>('[role="dialog"]:not([aria-modal="false"]), [role="alertdialog"]:not([aria-modal="false"])')].filter(visible).at(-1)
  const controls = new Map<string, HTMLElement[]>()
  for (const control of root.querySelectorAll<HTMLElement>('[data-native-action]')) {
    const id = control.dataset.nativeAction!
    const list = controls.get(id) ?? []
    list.push(control); controls.set(id,list)
  }
  return {composer,modal,controls}
}

/** Ambiguous targets are disabled, never resolved by choosing the first row. */
export function actionTarget(id: string, root: Document = document, scope = readScope(root)): Action | undefined {
  if (!ids.has(id)) return
  const {composer,modal} = scope
  // Dialog controls retain their own focused UI. Background commands must not
  // escape confirmations, source permissions or pickers.
  const allowRegistered = !modal || !!composer && (modal === composer || modal.contains(composer))
  const focused = root.activeElement
  if (!composer && (id==='edit.undo'||id==='composer.redo') && focused instanceof HTMLElement && (focused.matches('input,textarea,[contenteditable="true"]') || focused.closest('[contenteditable="true"]'))) {
    return {run:()=>root.execCommand(id==='edit.undo'?'undo':'redo'),available:()=>true,token:'text-edit:'+identity(focused)}
  }
  const candidates = allowRegistered ? [...(actions.get(id) ?? [])].filter(action => action.available()) : []
  if (['edit.cut','edit.copy','edit.selectAll','edit.paste'].includes(id)) {
    candidates.push({run:async()=>{
      const command=id.slice('edit.'.length)
      const active=root.activeElement
      if(command==='paste'){
        const before = active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement ? [active.value,active.selectionStart,active.selectionEnd].join('|') : root.getSelection()?.toString()
        const text=await ClipboardGetText()
        const after = active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement ? [active.value,active.selectionStart,active.selectionEnd].join('|') : root.getSelection()?.toString()
        if(root.activeElement===active && before===after) insertClipboardTextAtFocus(text)
        return
      }
      if(active instanceof HTMLIFrameElement && active.contentWindow){
        active.contentWindow.postMessage({type:'native-edit-command',command},'*')
        return
      }
      root.execCommand(command)
    },available:()=>true,token:'edit:'+identity(root.activeElement ?? root)})
  }
  // All ids are fixed catalog strings, not user content/selectors.
  const controls = (scope.controls.get(id) ?? [])
    .filter(element => element.isConnected && element.getClientRects().length > 0 && !element.closest('[hidden], [inert], [aria-hidden="true"]'))
    .filter(element => (!modal || modal.contains(element)) && (!composer || !!modal || composer.contains(element)))
  // A displayed but disabled control must never fall through to a registration.
  if (controls.length) {
    const buttons = controls.filter(visible)
    let target: HTMLElement | undefined
    if (buttons.length === 1) target = buttons[0]
    if (buttons.length > 1) {
      const focused = buttons.filter(button => button === root.activeElement || button.closest('[data-native-row]')?.contains(root.activeElement))
      if (focused.length === 1) target = focused[0]
    }
    return target ? { run: () => {
      // Bits UI picker triggers open on keydown/pointerdown, not click.
      if(['menu','listbox'].includes(target.getAttribute('aria-haspopup') ?? '')) {
        target.focus()
        target.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowDown',code:'ArrowDown',bubbles:true,cancelable:true}))
        return
      }
      target.click()
    }, available: () => visible(target), token: 'button:'+identity(target) } : undefined
  }
  return candidates.length === 1 ? { ...candidates[0], token:candidates[0].token??'action:'+identity(candidates[0]) } : undefined
}

/** Exported bridge has no business actions: tests exercise the same dispatcher. */
export function installNativeMenu() {
  // Epochs survive frontend reloads; a delayed state from a prior page cannot
  // re-enable commands after the new page has taken ownership.
  let epoch = Math.floor(performance.timeOrigin * 1000)
  let snapshotKey = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  const snapshot = () => {
    const scope=readScope(document)
    const targets = [...ids].map(id => [id, actionTarget(id,document,scope)] as const).filter(([,target]) => !!target)
    const context = [...document.querySelectorAll<HTMLElement>('[data-native-context]')]
      .filter(element => element.getClientRects().length > 0 && !element.closest('[hidden], [inert], [aria-hidden="true"]'))
      .map(element => element.dataset.nativeContext)
    const view = document.querySelector<HTMLElement>('[data-native-view]')?.dataset.nativeView ?? 'mail'
    return { view, enabled:targets.map(([id])=>id), key:JSON.stringify([view,context,targets.map(([id,target])=>[id,target?.token])]) }
  }
  const publish = () => {
    timer = undefined
    const state = snapshot()
    if (state.key === snapshotKey) return
    snapshotKey = state.key
    EventsEmit('native-menu:state', { epoch: ++epoch, enabled:state.enabled, view:state.view })
  }
  const changed = () => { if (!timer) timer = setTimeout(publish, 40) }
  invalidate = changed
  const observer = new MutationObserver(changed)
  observer.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true })
  document.addEventListener('focusin', changed)
  const cancel = EventsOn('native-menu:invoke', (request: { id?: string; epoch?: number }) => {
    // Recompute before every click. A new selection can expose identical
    // commands, but must not inherit an in-flight click from the old one.
    if (request.epoch !== epoch || snapshot().key !== snapshotKey || !request.id) { changed(); return }
    const target = actionTarget(request.id)
    if (!target) { changed(); return }
    try { Promise.resolve(target.run()).catch(error => console.error('[native menu]', error)) } catch (error) { console.error('[native menu]', error) }
    changed()
  })
  changed()
  return () => { cancel(); observer.disconnect(); document.removeEventListener('focusin', changed); if (timer) clearTimeout(timer); invalidate = () => {} }
}
