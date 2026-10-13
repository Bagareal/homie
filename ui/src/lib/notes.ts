// Release notes are plain text written as small Markdown: "### Title" starts a
// change, the lines under it are its description (paragraphs, and "- " lists).
// They are turned into blocks the page draws as text: nothing from the notes is
// ever put into the page as HTML.

export type NoteBlock = { kind: 'p'; text: string } | { kind: 'ul'; items: string[] }
export interface NoteSection {
  title: string
  blocks: NoteBlock[]
}

export function parseNotes(text: string): NoteSection[] {
  const sections: NoteSection[] = []
  let cur: NoteSection = { title: '', blocks: [] }
  let para: string[] = []
  let list: string[] | null = null

  const flushPara = () => {
    if (para.length) cur.blocks.push({ kind: 'p', text: para.join(' ') })
    para = []
  }
  const flushList = () => {
    if (list) cur.blocks.push({ kind: 'ul', items: list })
    list = null
  }
  const flush = () => {
    flushPara()
    flushList()
  }
  const start = (title: string) => {
    flush()
    if (cur.title || cur.blocks.length) sections.push(cur)
    cur = { title, blocks: [] }
  }

  for (const raw of (text ?? '').replace(/\r\n?/g, '\n').split('\n')) {
    const line = raw.trimEnd()
    const heading = /^#{1,4}\s+(.*)$/.exec(line)
    if (heading) {
      start(heading[1].trim())
    } else if (/^\*\*Full changelog:\*\*/i.test(line)) {
      // The link to the comparison is on the release page itself.
      continue
    } else if (line.trim() === '') {
      flush()
    } else if (/^\s*[-*]\s+/.test(line)) {
      flushPara()
      list ??= []
      list.push(line.replace(/^\s*[-*]\s+/, ''))
    } else if (list && /^\s+\S/.test(line)) {
      // The continuation of a list item.
      list[list.length - 1] += ` ${line.trim()}`
    } else {
      flushList()
      para.push(line.trim())
    }
  }
  flush()
  if (cur.title || cur.blocks.length) sections.push(cur)
  return sections
}

// v1.23.4 -> [1, 23, 4]; anything else (such as "dev") is not a version.
export function versionParts(v: string): [number, number, number] | null {
  const m = /^v(\d+)\.(\d+)\.(\d+)$/.exec(v ?? '')
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null
}

// -1, 0 or 1; a non-version is older than every version.
export function compareVersions(a: string, b: string): number {
  const pa = versionParts(a)
  const pb = versionParts(b)
  if (!pa && !pb) return 0
  if (!pa) return -1
  if (!pb) return 1
  for (let i = 0; i < 3; i++) if (pa[i] !== pb[i]) return pa[i] < pb[i] ? -1 : 1
  return 0
}
