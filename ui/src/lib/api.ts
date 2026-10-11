import type { Config, Status } from './types'

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

export const getVersion = () => request<{ version: string }>('api/version')
