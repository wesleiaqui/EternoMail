import { ChangeReplyLaterWithUndo, GetReplyLaterQueue } from '../../../wailsjs/go/app/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

export interface ReplyLaterRef { accountId: string; conversationId: string; indexed?: boolean }
type MembershipEvent = ReplyLaterRef & { inQueue: boolean }
const key = (ref: ReplyLaterRef) => JSON.stringify([ref.accountId, ref.conversationId])
let refs = $state<ReplyLaterRef[]>([])
let busy = $state<Set<string>>(new Set())
let ready = $state(false)
let error = $state('')
let loading: Promise<void> | null = null
let subscribed = false

function update(ref: ReplyLaterRef, inQueue: boolean) {
  const existing = refs.find(item => key(item) === key(ref))
  refs = refs.filter(item => key(item) !== key(ref))
  if (inQueue) refs = [{ ...existing, ...ref, indexed: ref.indexed ?? existing?.indexed }, ...refs]
}

async function load(force = false): Promise<void> {
  if (!subscribed) {
    subscribed = true
    EventsOn('replyLater:changed', (event: MembershipEvent) => update(event, event.inQueue))
    EventsOn('replyLater:synced', () => { void load(true).catch(() => {}) })
    EventsOn('folder:synced', (event: { accountId: string }) => {
      if (refs.some(ref => ref.accountId === event.accountId)) void load(true).catch(() => {})
    })
  }
  if (loading) return loading
  if (ready && !force) return
  loading = (async () => {
    try {
      refs = await GetReplyLaterQueue() || []
      ready = true
      error = ''
    } catch (err) {
      error = String(err)
      throw err
    } finally { loading = null }
  })()
  return loading
}

export const replyLaterQueue = {
  get refs() { return refs },
  get count() { return refs.length },
  get ready() { return ready },
  get error() { return error },
  load,
  has(accountId: string, conversationId: string) { return refs.some(ref => ref.accountId === accountId && ref.conversationId === conversationId) },
  isBusy(accountId: string, conversationId: string) { return busy.has(key({ accountId, conversationId })) },
  async set(accountId: string, conversationId: string, inQueue: boolean) {
    const ref = { accountId, conversationId }, id = key(ref)
    if (busy.has(id)) return null
    busy = new Set([...busy, id])
    try {
      const result = await ChangeReplyLaterWithUndo(accountId, conversationId, inQueue)
      update(ref, inQueue)
      return result
    } finally { busy = new Set([...busy].filter(item => item !== id)) }
  },
}
