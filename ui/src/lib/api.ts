import type { Config, Status, WidgetData } from './types'

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

export const testWidget = (w: { id?: string; url: string; username: string; password: string }) =>
  request<{ ok: boolean; version?: string; error?: string }>('api/widgets/test', {
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
