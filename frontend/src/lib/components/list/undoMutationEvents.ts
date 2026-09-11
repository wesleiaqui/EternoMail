export const UNDO_OPERATION_CREATED_EVENT = 'eterno-mail:undo-operation-created'
export const UNDO_OPERATION_COMPLETED_EVENT = 'eterno-mail:undo-operation-completed'

export type UndoOperationCreatedDetail = {
  operationId: string
  messageIds: string[]
  action: string
}

export function publishUndoOperationCreated(operationId: string, messageIds: string[], action: string) {
  if (!operationId || typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent<UndoOperationCreatedDetail>(UNDO_OPERATION_CREATED_EVENT, {
    detail: { operationId, messageIds: [...messageIds], action },
  }))
}

export function publishUndoOperationCompleted(operationId: string) {
  if (!operationId || typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent<string>(UNDO_OPERATION_COMPLETED_EVENT, {
    detail: operationId,
  }))
}
