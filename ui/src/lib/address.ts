// An address is written as host and port apart in the editor, and stored as one
// string, scheme://host:port[/path]. The port can be left out: the default one of
// the program is added when the widget is tested or saved.

const ADDRESS = /^((?:[a-z][a-z0-9+.-]*:\/\/)?[^/:\s]+)(?::(\d{1,5}))?(\/.*)?$/i

// "http://nas:61208" -> host "http://nas", port "61208". A path stays with the host.
export function splitAddress(value: string): { host: string; port: string } {
  const m = ADDRESS.exec((value ?? '').trim())
  if (!m) return { host: value ?? '', port: '' }
  return { host: m[1] + (m[3] ?? ''), port: m[2] ?? '' }
}

// The reverse of splitAddress; a port typed into the host field is kept if the port field is empty.
export function joinAddress(host: string, port: string): string {
  const m = ADDRESS.exec((host ?? '').trim())
  if (!m) return host ?? ''
  const p = port.trim() || m[2] || ''
  return m[1] + (p ? `:${p}` : '') + (m[3] ?? '')
}

// Adds the port a program listens on by default when the address has none.
export function withDefaultPort(value: string, port: string): string {
  const m = ADDRESS.exec((value ?? '').trim())
  if (!m || m[2] || !port) return value
  return `${m[1]}:${port}${m[3] ?? ''}`
}
