import { installNativeMenu } from './lib/nativeMenu'
import './app.css'
import './lib/iconify-offline'
import { mount } from 'svelte'
import { initI18n } from './lib/i18n'
import ReaderApp from './ReaderApp.svelte'
import { loadSettings } from './lib/stores/settings.svelte'
import { loadUIState } from './lib/stores/uiState.svelte'
import { initTheme } from './lib/stores/theme.svelte'
import { loadImageAllowlist } from './lib/stores/imageAllowlist.svelte'
import { accountStore } from './lib/stores/accounts.svelte'
import { GetAccounts, GetSystemTheme } from '../wailsjs/go/app/App'

interface ReaderBackend {
  GetConfig(): Promise<{ accountId: string; folderId: string; threadId: string; folderType: string }>
  Call(operation: string, args: unknown[]): Promise<unknown>
}

async function bootstrap() {
  const host = window as unknown as { go: { app: { ReaderApp: ReaderBackend; App: unknown } } }
  const deadline = Date.now() + 10000
  while (!host.go?.app?.ReaderApp) {
    if (Date.now() > deadline) throw new Error('Reader runtime unavailable')
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  const backend = host.go.app.ReaderApp
  // Existing generated App functions are reused by the viewer. The child binds
  // only ReaderApp; every operation is checked by the parent's explicit allowlist.
  host.go.app.App = new Proxy({}, { get: (_target, operation: string) => {
    // Reader-only preference changes must never persist the main window layout.
    if (operation === 'SaveUIState') return async () => undefined
    // The detached reader has no independent layout to restore. Avoid blocking
    // its first paint on a parent database read for the main window's UI state.
    if (operation === 'GetUIState') return async () => ({})
    return (...args: unknown[]) => backend.Call(operation, args)
  } })
  const config = await backend.GetConfig()
  await initI18n()
  const theme = await loadSettings()
  await Promise.all([loadUIState(), loadImageAllowlist()])
  const accounts = await GetAccounts()
  accountStore.accounts = (accounts || []).map(account => ({ account, folders: [], loading: false, syncing: false, error: null, lastSync: null }))
  await initTheme(theme, GetSystemTheme)
  installNativeMenu()
  mount(ReaderApp, { target: document.getElementById('app')!, props: { config } })
  document.getElementById('boot-status')?.remove()
}
bootstrap().catch(error => {
  console.error('[reader]', error)
  const message = `Postilaatikko: ${error instanceof Error ? error.message : String(error)}`
  const status = document.getElementById('boot-status')
  if (status) {
    status.textContent = message
  } else {
    document.getElementById('app')!.textContent = message
  }
})
