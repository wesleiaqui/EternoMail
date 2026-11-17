// Presentation compatibility layer: no data migration, old clients keep working.
import { readLaterQueue } from './readLater.svelte'
import { replyLaterQueue } from './replyLater.svelte'
import { UndoOperation, ChangeLaterWithUndo } from '../../../wailsjs/go/app/App'

export type LaterRef = { accountId: string; conversationId: string; indexed?: boolean }
export type LaterChange = { changed: boolean; operationId: string; operationIds: string[] }
type Ref = LaterRef
const key = (ref: Ref) => JSON.stringify([ref.accountId, ref.conversationId])
let busy = $state<Set<string>>(new Set())

export async function undoLaterChange(result: Pick<LaterChange, 'operationIds'>) {
  // Each conversation is one backend-owned operation, including both legacy flags.
  for (const id of [...result.operationIds].reverse()) await UndoOperation(id)
}

async function load(force = false) {
  await Promise.all([readLaterQueue.load(force), replyLaterQueue.load(force)])
}

async function setWithUndo(accountId: string, conversationId: string, inQueue: boolean): Promise<LaterChange | null> {
  const id = key({ accountId, conversationId })
  if (busy.has(id)) return null
  busy = new Set([...busy, id])
  try {
    const result = await ChangeLaterWithUndo(accountId, conversationId, inQueue)
    return { ...result, operationIds: result.changed && result.operationId ? [result.operationId] : [] }
  } finally {
    busy = new Set([...busy].filter(item => item !== id))
    await load(true).catch(() => {})
  }
}

export const laterQueue = {
  get refs(): Ref[] {
    const refs = new Map<string, Ref>()
    for (const ref of [...readLaterQueue.refs, ...replyLaterQueue.refs]) {
      const previous = refs.get(key(ref))
      refs.set(key(ref), { ...previous, ...ref, indexed: previous?.indexed === true || ref.indexed === true ? true : ref.indexed ?? previous?.indexed })
    }
    return [...refs.values()]
  },
  get count() { return this.refs.length },
  get ready() { return readLaterQueue.ready && replyLaterQueue.ready },
  get error() { return replyLaterQueue.error },
  load,
  has(accountId: string, conversationId: string) { return readLaterQueue.has(accountId, conversationId) || replyLaterQueue.has(accountId, conversationId) },
  isBusy(accountId: string, conversationId: string) { return busy.has(key({ accountId, conversationId })) || readLaterQueue.isBusy(accountId, conversationId) || replyLaterQueue.isBusy(accountId, conversationId) },
  setWithUndo,
  set: setWithUndo,
}
