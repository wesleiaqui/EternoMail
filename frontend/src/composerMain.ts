import { installNativeMenu } from './lib/nativeMenu'
import './app.css'
import { initI18n } from './lib/i18n'
import ComposerApp from './ComposerApp.svelte'
import { mount } from 'svelte'
// @ts-ignore - Wails runtime
import { ClipboardGetText, EventsOn } from '../wailsjs/runtime/runtime'
import { insertClipboardTextAtFocus } from './lib/nativePaste'

// Initialize i18n and wait for locale to load before mounting
// ($_ throws if called before the locale is ready)
async function bootstrap() {
  await waitForRuntime()
  EventsOn('native:paste-text', (text: string) => insertClipboardTextAtFocus(text))
  EventsOn('native:paste-requested', async () => {
    if (!document.hasFocus()) return
    try {
      insertClipboardTextAtFocus(await ClipboardGetText())
    } catch (error) {
      console.warn('[composer] clipboard text unavailable', error)
    }
  })
  await initI18n()
  installNativeMenu()
  mount(ComposerApp, { target: document.getElementById('app')! })
}

function waitForRuntime(): Promise<void> {
  return new Promise(resolve => {
    const startedAt = performance.now()
    const check = () => {
      const runtime = (window as unknown as { runtime?: { EventsOn?: unknown } }).runtime
      if (runtime?.EventsOn || performance.now() - startedAt > 2000) {
        resolve()
        return
      }
      requestAnimationFrame(check)
    }
    check()
  })
}

bootstrap()
