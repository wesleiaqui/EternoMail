import { ChangeReadLater, GetReadLaterQueue } from '../../../wailsjs/go/app/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

export interface ReadLaterRef { accountId: string; conversationId: string }
type MembershipEvent = ReadLaterRef & { inQueue: boolean }
const key = (ref: ReadLaterRef) => JSON.stringify([ref.accountId, ref.conversationId])
let refs = $state<ReadLaterRef[]>([])
let busy = $state<Set<string>>(new Set())
let ready = $state(false)
let loading: Promise<void> | null = null
let subscribed = false

function update(ref: ReadLaterRef, inQueue: boolean) {
  refs = refs.filter(item => key(item) !== key(ref))
  if (inQueue) refs = [ref, ...refs]
}

async function load(force = false): Promise<void> {
  if (!subscribed) {
    subscribed = true
    EventsOn('readLater:changed', (event: MembershipEvent) => update(event, event.inQueue))
    EventsOn('readLater:synced', () => { void load(true).catch(() => {}) })
    EventsOn('snooze:expired', () => { void load(true).catch(() => {}) })
  }
  if (loading) return loading
  if (ready && !force) return
  loading = (async () => {
    try { refs = await GetReadLaterQueue() || []; ready = true }
    finally { loading = null }
  })()
  return loading
}

export const readLaterQueue = {
  get refs() { return refs },
  get count() { return refs.length },
  get ready() { return ready },
  load,
  has(accountId: string, conversationId: string) { return refs.some(ref => key(ref) === key({ accountId, conversationId })) },
  isBusy(accountId: string, conversationId: string) { return busy.has(key({ accountId, conversationId })) },
  async set(accountId: string, conversationId: string, inQueue: boolean) {
    const ref = { accountId, conversationId }, id = key(ref)
    if (busy.has(id)) return false
    busy = new Set([...busy, id])
    try { const changed = await ChangeReadLater(accountId, conversationId, inQueue); update(ref, inQueue); return changed }
    finally { busy = new Set([...busy].filter(item => item !== id)) }
  },
}
