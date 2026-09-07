import { writable } from 'svelte/store'

export interface ToastAction {
  label: string
  onClick: () => void
}

export interface Toast {
  id: string
  message: string
  type: 'success' | 'error' | 'info' | 'warning'
  duration?: number
  actions?: ToastAction[]
}

function createToastStore() {
  const { subscribe, update } = writable<Toast[]>([])

  // Keep one timer per toast so replacing an existing notification can also
  // restart its lifetime instead of letting an older timer remove it.
  const timers = new Map<string, ReturnType<typeof setTimeout>>()

  function getDuration(toast: Toast): number {
    return toast.duration ??
      (toast.actions && toast.actions.length > 0 ? 8000 : 5000)
  }

  function scheduleRemoval(toast: Toast) {
    const existingTimer = timers.get(toast.id)

    if (existingTimer !== undefined) {
      clearTimeout(existingTimer)
    }

    const timer = setTimeout(() => {
      timers.delete(toast.id)
      update(toasts => toasts.filter(t => t.id !== toast.id))
    }, getDuration(toast))

    timers.set(toast.id, timer)
  }

  function add(toast: Omit<Toast, 'id'>) {
    const id = crypto.randomUUID()
    const newToast: Toast = { ...toast, id }

    update(toasts => [...toasts, newToast])
    scheduleRemoval(newToast)

    return id
  }

  function remove(id: string) {
    const timer = timers.get(id)

    if (timer !== undefined) {
      clearTimeout(timer)
      timers.delete(id)
    }

    update(toasts => toasts.filter(t => t.id !== id))
  }

  // Update an existing toast in place.
  //
  // The patch is intentionally partial: callers may change only the message,
  // actions, type or duration while preserving every other property.
  function replace(
    id: string,
    patch: Partial<Omit<Toast, 'id'>>
  ): boolean {
    let replacedToast: Toast | null = null

    update(toasts =>
      toasts.map(toast => {
        if (toast.id !== id) {
          return toast
        }

        replacedToast = {
          ...toast,
          ...patch,
          id
        }

        return replacedToast
      })
    )

    if (replacedToast === null) {
      return false
    }

    scheduleRemoval(replacedToast)
    return true
  }

  function success(message: string, actions?: ToastAction[]) {
    return add({ message, type: 'success', actions })
  }

  function error(message: string, actions?: ToastAction[]) {
    return add({ message, type: 'error', actions })
  }

  function info(message: string, actions?: ToastAction[]) {
    return add({ message, type: 'info', actions })
  }

  function warning(message: string, actions?: ToastAction[]) {
    return add({ message, type: 'warning', actions })
  }

  return {
    subscribe,
    add,
    remove,
    replace,
    success,
    error,
    info,
    warning
  }
}

export const toasts = createToastStore()

// Helper function for easy toast creation
export function addToast(toast: Omit<Toast, 'id'>) {
  return toasts.add(toast)
}
