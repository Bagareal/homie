import type { Config, HostData, Status, WidgetData } from './types'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new Error('the Homie server is not responding')
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) throw new Error(body?.error ?? `HTTP error ${res.status}`)
  return body as T
}

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
  only?: 'glances'
  // The extra Glances source, tested along with the main one.
  glances?: { url: string; username: string; password: string; nodes?: Record<string, string> }
}) =>
  request<ProbeResult & { extra?: { glances?: ProbeResult; nodes?: Record<string, ProbeResult> } }>('api/widgets/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(w),
  })

export const getVersion = () => request<{ version: string }>('api/version')

export const uploadBackground = (file: File) => {
  const form = new FormData()
  form.append('file', file)
  return request<Config>('api/background', { method: 'POST', body: form })
}

export const removeBackground = () => request<Config>('api/background', { method: 'DELETE' })
