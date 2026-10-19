import type { Config, HAEntity, HostData, Status, WidgetData } from './types'

// Set by the login state: told when the server says the session is gone (401) or
// locked (423), so the page can show the sign-in or the biometric screen.
export const hooks: { onAuth: (state: 'login' | 'locked' | 'setup') => void } = { onAuth: () => {} }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new Error('the Homie server is not responding')
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    // The sign-in calls report their own errors (a wrong password is not a lost session).
    if ((res.status === 401 || res.status === 423) && !path.startsWith('api/auth/')) {
      hooks.onAuth(res.status === 423 ? 'locked' : body?.state === 'setup' ? 'setup' : 'login')
    }
    throw new Error(body?.error ?? `HTTP error ${res.status}`)
  }
  return body as T
}

const send = <T>(path: string, method: string, body?: unknown) =>
  request<T>(path, { method, headers: body === undefined ? undefined : { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })

// ---- login

export interface AuthStatus {
  state: 'setup' | 'login' | 'locked' | 'ok'
  username?: string
  // The account was reset from the terminal: creating the new one needs its code.
  needCode?: boolean
  // Passkeys need https (or localhost).
  secure: boolean
  // This device has the biometric lock, and how long it may be out of sight before it closes.
  biometric: boolean
  lockAfter?: number
  device?: string
}

export interface Device {
  id: string
  label: string
  created: string
  lastUsed: string
  current: boolean
  lock: boolean
}

export interface PasskeyInfo {
  id: string
  name: string
  created: string
  used: number
}

// What navigator.credentials.create()/get() are given, with binary values in base64url.
export interface PasskeyOptions {
  challenge: string
  rp?: { name: string }
  user?: { id: string; name: string; displayName: string }
  pubKeyCredParams?: { type: 'public-key'; alg: number }[]
  excludeCredentials?: { type: 'public-key'; id: string }[]
  allowCredentials?: { type: 'public-key'; id: string }[]
  authenticatorSelection?: Record<string, string>
  attestation?: string
  userVerification?: string
  timeout: number
}

export interface PasskeyCredential {
  id: string
  response: { clientDataJSON: string; attestationObject?: string; authenticatorData?: string; signature?: string }
}

export const authStatus = () => request<AuthStatus>('api/auth/status')
export const authSetup = (username: string, password: string, code: string) => send<AuthStatus>('api/auth/setup', 'POST', { username, password, code })
export const authLogin = (username: string, password: string) => send<AuthStatus>('api/auth/login', 'POST', { username, password })
export const authLogout = () => send<{ state: string }>('api/auth/logout', 'POST')
export const authActive = () => send<{ state: AuthStatus['state'] }>('api/auth/active', 'POST')
export const authChangePassword = (current: string, next: string) => send<{ ok: boolean }>('api/auth/password', 'POST', { current, next })
export const authDevices = () => request<{ devices: Device[]; passkeys: PasskeyInfo[]; username: string }>('api/auth/devices')
export const authRevoke = (id: string) => send<{ ok: boolean }>(`api/auth/devices/${encodeURIComponent(id)}`, 'DELETE')
export const authRevokeOthers = () => send<{ ok: boolean }>('api/auth/devices/others/revoke', 'POST')
export const passkeyRegisterBegin = () => send<PasskeyOptions>('api/auth/passkey/register/begin', 'POST')
export const passkeyRegisterFinish = (credential: PasskeyCredential, name: string, lockAfter: number) =>
  send<AuthStatus>('api/auth/passkey/register/finish', 'POST', { credential, name, lockAfter })
export const passkeyAssertBegin = (link = false) => send<PasskeyOptions>(`api/auth/passkey/assert/begin${link ? '?link=1' : ''}`, 'POST')
export const passkeyAssertFinish = (credential: PasskeyCredential, link = false, lockAfter = 60) =>
  send<AuthStatus>('api/auth/passkey/assert/finish', 'POST', { credential, link, lockAfter })
export const passkeyRemove = (id: string) => send<{ ok: boolean }>(`api/auth/passkeys/${encodeURIComponent(id)}`, 'DELETE')
export const lockOff = () => send<AuthStatus>('api/auth/lock/off', 'POST')
export const lockAfter = (sec: number) => send<AuthStatus>('api/auth/lock/after', 'POST', { lockAfter: sec })

// ---- Home Assistant

export const widgetCommand = (widgetId: string, row: string, action: 'toggle' | 'on' | 'off' | 'trigger') =>
  send<{ ok: boolean; entity?: HAEntity }>(`api/widgets/${encodeURIComponent(widgetId)}/command`, 'POST', { row, action })

// Relative paths: Homie also works behind a reverse proxy on a subpath.
export const getConfig = () => request<Config>('api/config')

export const putConfig = (cfg: Config) =>
  request<Config>('api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  })

export const getStatus = () => request<Record<string, Status>>('api/status')

export const getWidgets = () => request<Record<string, WidgetData>>('api/widgets')

export interface ProbeResult {
  ok: boolean
  version?: string
  error?: string
  data?: HostData
}

export const testWidget = (w: {
  id?: string
  kind: string
  url: string
  username: string
  password: string
  // "glances": test only the extra source (the editor has a Test button for each source).
  only?: 'glances' | 'nut'
  // The extra UPS source (an address).
  nut?: { url: string }
  // The extra Glances source, tested along with the main one.
  glances?: { url: string; username: string; password: string; nodes?: Record<string, string> }
}) =>
  request<ProbeResult & { extra?: { glances?: ProbeResult; nodes?: Record<string, ProbeResult>; nut?: ProbeResult } }>('api/widgets/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(w),
  })

export interface Release {
  version: string
  url: string
  date: string
  notes: string
}

// refresh asks the server to read GitHub again now (it does so at most once a minute).
export const getReleases = (refresh = false) =>
  request<{ current: string; enabled: boolean; checkedAt?: string; releases: Release[]; error?: string }>(`api/releases${refresh ? '?refresh=1' : ''}`)

export const getVersion = () => request<{ version: string }>('api/version')

export const uploadBackground = (file: File) => {
  const form = new FormData()
  form.append('file', file)
  return request<Config>('api/background', { method: 'POST', body: form })
}

export const removeBackground = () => request<Config>('api/background', { method: 'DELETE' })
