import * as api from './api'
import { passkeyError, usePasskey } from './webauthn'
import { app } from './state.svelte'

export type AuthState = 'loading' | 'setup' | 'login' | 'locked' | 'ok'

// How often a page that is in front of the user says so, for the biometric lock.
const BEAT_MS = 20_000

class Auth {
  state = $state<AuthState>('loading')
  username = $state('')
  needCode = $state(false)
  secure = $state(false)
  // This device has the biometric lock, and after how many seconds out of sight it closes.
  biometric = $state(false)
  lockAfter = $state(60)
  device = $state('')
  #beat: ReturnType<typeof setInterval> | undefined
  #started = false

  constructor() {
    api.hooks.onAuth = (s) => this.lost(s)
  }

  apply(s: api.AuthStatus) {
    this.username = s.username ?? ''
    this.needCode = !!s.needCode
    this.secure = s.secure
    this.biometric = s.biometric
    this.lockAfter = s.lockAfter || 60
    this.device = s.device ?? ''
    this.state = s.state
    if (s.state !== 'ok') app.reset()
    this.#watch()
  }

  async refresh() {
    try {
      this.apply(await api.authStatus())
    } catch {
      // The server is not answering: say so instead of showing a sign-in that cannot work.
      this.state = 'login'
      throw new Error('the Homie server is not responding')
    }
  }

  // The server said the session is gone, or locked.
  lost(state: 'login' | 'locked' | 'setup') {
    if (this.state === state) return
    this.state = state
    app.reset()
    this.#watch()
  }

  async setup(username: string, password: string, code: string) {
    this.apply(await api.authSetup(username, password, code))
  }

  async login(username: string, password: string) {
    this.apply(await api.authLogin(username, password))
  }

  async logout() {
    try {
      await api.authLogout()
    } finally {
      await this.refresh().catch(() => {})
    }
  }

  // Asks the device for its biometric and opens the lock.
  async unlock(): Promise<string | null> {
    try {
      const o = await api.passkeyAssertBegin()
      const cred = await usePasskey(o)
      this.apply(await api.passkeyAssertFinish(cred))
      return null
    } catch (e) {
      return passkeyError(e)
    }
  }

  // The page tells the server it is in front of the user (every few seconds) and
  // when it is not (the moment it goes out of sight): the lock counts from there.
  #watch() {
    clearInterval(this.#beat)
    this.#beat = undefined
    if (this.state !== 'ok' || !this.biometric) return
    this.#beat = setInterval(() => {
      if (document.visibilityState === 'visible') void this.#active()
    }, BEAT_MS)
    if (!this.#started) {
      this.#started = true
      document.addEventListener('visibilitychange', () => {
        if (this.state !== 'ok' || !this.biometric) return
        if (document.visibilityState === 'hidden') this.#hidden()
        else void this.#active()
      })
      window.addEventListener('pagehide', () => {
        if (this.state === 'ok' && this.biometric) this.#hidden()
      })
    }
  }

  async #active() {
    try {
      const r = await api.authActive()
      if (r.state !== 'ok') this.lost(r.state === 'locked' ? 'locked' : 'login')
    } catch {
      // Offline: the next beat tries again.
    }
  }

  #hidden() {
    try {
      // A beacon still goes out while the page is being closed or put away.
      navigator.sendBeacon('api/auth/hidden')
    } catch {
      // Nothing to do: the missing beats lock it a little later.
    }
  }
}

export const auth = new Auth()
