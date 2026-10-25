// Mail helpers shared by the pages: views, dates, people, actions with Undo, live updates, the composer.
import { useEffect, useRef, type ReactNode } from 'react'
import { api, V1 } from './api'
import { fmtTime, fmtYMD } from './format'
import type { ActionRequest, ActionResult, Address, AppsStatus, Box, Category, ComposeRequest, FolderRole, LiveEvent, Message } from './types'

// ---- views ----

export const BOXES: { id: Box; label: string; icon: string; count?: string }[] = [
  { id: 'inbox', label: 'Inbox', icon: 'inbox', count: 'inbox' },
  { id: 'unread', label: 'Unread', icon: 'mail', count: 'unread' },
  { id: 'starred', label: 'Starred', icon: 'star' },
  { id: 'snoozed', label: 'Snoozed', icon: 'alarm', count: 'snoozed' },
  { id: 'drafts', label: 'Drafts', icon: 'file-text', count: 'drafts' },
  { id: 'sent', label: 'Sent', icon: 'send' },
  { id: 'archive', label: 'Archive', icon: 'archive' },
  { id: 'spam', label: 'Spam', icon: 'spam', count: 'spam' },
  { id: 'trash', label: 'Trash', icon: 'trash' },
]

export const BOX_LABEL: Record<string, string> = {
  inbox: 'Inbox', unread: 'Unread', starred: 'Starred', snoozed: 'Snoozed', drafts: 'Drafts', sent: 'Sent', archive: 'Archive',
  spam: 'Spam', trash: 'Trash', all: 'All mail', attachments: 'Attachments', receipts: 'Receipts', newsletters: 'Newsletters',
  updates: 'Updates', people: 'People', vip: 'VIP',
}

export const CATEGORIES: { id: Category; label: string; icon: string }[] = [
  { id: 'people', label: 'People', icon: 'users' },
  { id: 'updates', label: 'Updates', icon: 'bell' },
  { id: 'newsletters', label: 'Newsletters', icon: 'rss' },
  { id: 'receipts', label: 'Receipts', icon: 'receipt' },
]

export const ROLE_ICON: Record<string, string> = { inbox: 'inbox', sent: 'send', drafts: 'file-text', trash: 'trash', junk: 'spam', archive: 'archive', all: 'archive', flagged: 'star', important: 'flag', '': 'folder' }
export const ROLE_LABEL: Partial<Record<FolderRole, string>> = { inbox: 'Inbox', sent: 'Sent', drafts: 'Drafts', trash: 'Trash', junk: 'Spam', archive: 'Archive', all: 'All mail', flagged: 'Starred', important: 'Important' }

/** The empty view, in plain words. */
export const EMPTY: Record<string, [string, string]> = {
  inbox: ['Inbox zero. Nice.', 'Nothing waiting for you. New mail shows up here by itself.'],
  unread: ['All read', 'Every message has been opened.'],
  starred: ['No starred mail', 'Star a message to keep it here (s).'],
  snoozed: ['Nothing snoozed', 'Snooze a message and it comes back to the inbox when you want it.'],
  drafts: ['No drafts', 'What you start writing is saved here as you type.'],
  sent: ['Nothing sent yet', ''],
  archive: ['The archive is empty', 'Archived mail lands here (e).'],
  spam: ['No spam', 'Mail your server thinks is junk lands here.'],
  trash: ['Trash is empty', ''],
  attachments: ['No attachments', ''],
  receipts: ['No receipts', 'Order confirmations, invoices and payment mails show up here.'],
  newsletters: ['No newsletters', ''],
  updates: ['No updates', ''],
  people: ['No mail from people', ''],
  search: ['Nothing found', 'Try fewer words, or look in the help for search operators.'],
  folder: ['This folder is empty', ''],
}

// ---- dates ----

const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()

/** A list date: the time today, "Yesterday", the weekday this week, else the date. */
export function mailDate(iso: string): string {
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return ''
  const now = new Date()
  if (sameDay(t, now)) return fmtTime(t)
  const y = new Date(now); y.setDate(now.getDate() - 1)
  if (sameDay(t, y)) return 'Yesterday'
  const diff = (now.getTime() - t.getTime()) / 86400000
  if (diff < 6 && diff > 0) return t.toLocaleDateString(undefined, { weekday: 'short' })
  return fmtYMD(t.getFullYear(), t.getMonth() + 1, t.getDate(), t.getFullYear() !== now.getFullYear())
}

/** The reader's date: "Mon 12 Oct 2026, 14:05 (3 h ago)". */
export function longDate(iso: string): string {
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return ''
  const day = t.toLocaleDateString(undefined, { weekday: 'short' })
  return `${day} ${fmtYMD(t.getFullYear(), t.getMonth() + 1, t.getDate())}, ${fmtTime(t)}`
}

// ---- snooze / schedule times ----

export function at(daysAhead: number, hour: number, minute = 0): Date {
  const d = new Date(); d.setDate(d.getDate() + daysAhead); d.setHours(hour, minute, 0, 0); return d
}

export function snoozeChoices(): { label: string; hint: string; when: Date }[] {
  const now = new Date()
  const out: { label: string; hint: string; when: Date }[] = []
  const later = new Date(now.getTime() + 3 * 3600000); later.setMinutes(0, 0, 0)
  if (later.getHours() < 21 && sameDay(later, now)) out.push({ label: 'Later today', hint: fmtTime(later), when: later })
  const tom = at(1, 8)
  out.push({ label: 'Tomorrow morning', hint: `${tom.toLocaleDateString(undefined, { weekday: 'short' })} ${fmtTime(tom)}`, when: tom })
  const dow = now.getDay() // 0 Sun
  if (dow >= 1 && dow <= 4) { const sat = at(6 - dow, 9); out.push({ label: 'This weekend', hint: `Sat ${fmtTime(sat)}`, when: sat }) }
  const mon = at(((8 - dow) % 7) || 7, 8)
  out.push({ label: 'Next week', hint: `Mon ${fmtTime(mon)}`, when: mon })
  return out
}

export function scheduleChoices(): { label: string; hint: string; when: Date }[] {
  const now = new Date()
  const out: { label: string; hint: string; when: Date }[] = []
  const tom = at(1, 8)
  out.push({ label: 'Tomorrow morning', hint: `${tom.toLocaleDateString(undefined, { weekday: 'short' })} ${fmtTime(tom)}`, when: tom })
  const aft = at(1, 13)
  out.push({ label: 'Tomorrow afternoon', hint: fmtTime(aft), when: aft })
  const dow = now.getDay()
  const mon = at(((8 - dow) % 7) || 7, 8)
  out.push({ label: 'Monday morning', hint: `Mon ${fmtTime(mon)}`, when: mon })
  return out
}

/** "2026-10-25T08:00" (local, for <input type=datetime-local>) */
export function localInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

// ---- people ----

export function who(a: Address | undefined): string {
  if (!a) return ''
  return a.name || a.addr
}

export function firstName(a: Address | undefined): string {
  const n = who(a)
  if (!n) return ''
  if (n.includes('@')) return n.split('@')[0]
  return n.split(/\s+/)[0]
}

/** "Ana, Marko, me" for a conversation row. */
export function rowSender(m: Message, mine: Set<string>): string {
  if (m.folder_role === 'sent' || m.folder_role === 'drafts' || mine.has(m.from.addr.toLowerCase())) {
    const to = m.to[0]
    return to ? `To: ${who(to)}${m.to.length > 1 ? ` +${m.to.length - 1}` : ''}` : 'Me'
  }
  return who(m.from)
}

export function fmtAddr(a: Address): string {
  if (!a.name || a.name === a.addr) return a.addr
  return `${a.name} <${a.addr}>`
}

export function joinAddrs(list: Address[]): string { return list.map(who).join(', ') }

const EMAIL_RE = /^[^\s@<>(),;:"]+@[^\s@<>(),;:"]+\.[^\s@<>(),;:"]{2,}$/
export const validEmail = (s: string) => EMAIL_RE.test(s.trim())

/** Parse "Ana <ana@x.org>, b@y.com; …" into addresses (invalid parts are returned as rest). */
export function parseAddrs(text: string): { list: Address[]; rest: string } {
  const list: Address[] = []
  const bad: string[] = []
  for (const raw of text.split(/[,;\n]+/)) {
    const part = raw.trim()
    if (!part) continue
    const m = part.match(/^\s*"?([^"<]*?)"?\s*<([^>]+)>\s*$/)
    if (m && validEmail(m[2])) list.push({ name: m[1].trim(), addr: m[2].trim() })
    else if (validEmail(part)) list.push({ name: '', addr: part })
    else bad.push(part)
  }
  return { list, rest: bad.join(', ') }
}

/** A stable colour for a person (the avatar's). */
export function hueOf(s: string): number { return [...s.toLowerCase()].reduce((a, c) => (a * 31 + c.charCodeAt(0)) % 360, 7) }

export const ACCOUNT_COLORS = ['#0b6bcb', '#7c3aed', '#16a34a', '#ea580c', '#db2777', '#0891b2', '#ca8a04', '#64748b']
export function accountColor(a: { color: string; id: number }): string { return a.color || ACCOUNT_COLORS[(a.id - 1) % ACCOUNT_COLORS.length] }

// ---- attachments ----

export function fileIcon(mime: string, name = ''): string {
  if (mime.startsWith('image/')) return 'image'
  if (mime === 'text/calendar' || name.endsWith('.ics')) return 'calendar'
  if (mime === 'application/pdf') return 'file-text'
  if (mime.includes('zip') || mime.includes('compressed')) return 'archive'
  if (mime.includes('sheet') || mime.includes('excel') || name.endsWith('.csv')) return 'grid'
  if (mime.startsWith('audio/')) return 'music'
  if (mime.startsWith('video/')) return 'video'
  return 'file'
}

export const attURL = (id: number, download = false) => `${V1}/attachments/${id}${download ? '?download=1' : ''}`

// ---- events between parts of the app ----

/** Something changed (an action, a send): lists, counts and the sidebar reload quietly. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pml:changed'))

export function useWindowEvent<T = unknown>(name: string, cb: (detail: T) => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = (e: Event) => ref.current((e as CustomEvent<T>).detail)
    window.addEventListener(name, h)
    return () => window.removeEventListener(name, h)
  }, [name])
}

export const useOnChange = (cb: () => void) => useWindowEvent('pml:changed', cb)
/** Live updates from the server (Layout owns the EventSource and re-broadcasts here). */
export const useLive = (cb: (e: LiveEvent) => void) => useWindowEvent<LiveEvent>('pml:live', cb)

export type ComposeOpen = ComposeRequest | { draft_id: number }
/** Open the composer from anywhere: a new message, a reply, a forward, or a saved draft. */
export const openCompose = (req: ComposeOpen) => window.dispatchEvent(new CustomEvent('pml:compose', { detail: req }))

// ---- actions with Undo ----

type Toast = (msg: ReactNode, kind?: 'ok' | 'err', action?: { label: string; run: () => void }) => void

let lastUndo: (() => void) | null = null
/** z: take back the last change. */
export function undoLast() { const f = lastUndo; lastUndo = null; f?.() }

const PAST: Partial<Record<ActionRequest['action'], string>> = {
  archive: 'Archived', trash: 'Moved to Trash', spam: 'Marked as spam', notspam: 'Moved to the inbox', inbox: 'Moved to the inbox', delete: 'Deleted for good',
  move: 'Moved', snooze: 'Snoozed', unsnooze: 'Back in the inbox', read: 'Marked as read', unread: 'Marked as unread', star: 'Starred', unstar: 'Star removed',
  pin: 'Pinned', unpin: 'Unpinned', category: 'Moved',
}

/** Run an action; moving ones show a toast with Undo. Returns the result (null on error). */
export async function act(req: ActionRequest, toast: Toast, opts: { quiet?: boolean; label?: string } = {}): Promise<ActionResult | null> {
  try {
    const r = await api.post<ActionResult>(`${V1}/actions`, req)
    announceChange()
    const undoable = r.ops?.length > 0 || req.action === 'snooze' || req.action === 'pin' || req.action === 'category'
    const undo = async () => {
      try {
        if (r.ops?.length) await api.post(`${V1}/undo`, { ops: r.ops })
        else if (req.action === 'snooze') await api.post(`${V1}/actions`, { ...req, action: 'unsnooze' })
        else if (req.action === 'pin') await api.post(`${V1}/actions`, { ...req, action: 'unpin' })
        announceChange()
        toast('Undone')
      } catch (e) { toast((e as Error).message, 'err') }
    }
    if (undoable) lastUndo = undo
    if (!opts.quiet) {
      const n = r.affected?.length || req.ids.length
      const what = opts.label || r.message || `${PAST[req.action] || 'Done'}${n > 1 ? ` · ${n}` : ''}`
      toast(what, 'ok', undoable ? { label: 'Undo', run: undo } : undefined)
    }
    return r
  } catch (e) {
    toast((e as Error).message, 'err')
    return null
  }
}

// ---- the apps the reader can hand things to ----

let appsCache: { at: number; p: Promise<AppsStatus> } | null = null
export function loadApps(force = false): Promise<AppsStatus> {
  if (!force && appsCache && Date.now() - appsCache.at < 60000) return appsCache.p
  const p = api.get<AppsStatus>(`${V1}/apps`).catch(() => ({ apps: {}, ai: { ready: false, provider: '', detail: 'not available' }, core: false } as AppsStatus))
  appsCache = { at: Date.now(), p }
  return p
}

export const APP_NAME: Record<string, string> = { documents: 'Pocket Documents', files: 'Pocket Files', photos: 'Photos', tasks: 'Pocket Tasks', calendar: 'Pocket Calendar', contacts: 'Pocket Contacts', notes: 'Pocket Notes', money: 'Pocket Money' }
export const APP_ICON: Record<string, string> = { documents: 'file-text', files: 'folder', photos: 'image', tasks: 'check-square', calendar: 'calendar', contacts: 'user', notes: 'note', money: 'wallet' }

// ---- text ----

/** Words of a search to highlight (operators dropped). */
export function searchWords(q: string): string[] {
  return q.replace(/\b\w+:("[^"]*"|\S+)/g, ' ').split(/\s+/).map(w => w.trim()).filter(w => w.length > 1)
}

export function escapeHTML(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

/** Plain text → safe HTML for the composer. */
export function textToHTML(s: string): string {
  return s.split(/\n{2,}/).map(p => `<p>${escapeHTML(p).replace(/\n/g, '<br>')}</p>`).join('')
}

/** Does the text talk about an attachment? (English and the languages around here.) */
export function mentionsAttachment(html: string): boolean {
  const text = html.replace(/<blockquote[\s\S]*?<\/blockquote>/gi, ' ').replace(/<[^>]+>/g, ' ').toLowerCase()
  return /\b(attached|attachment|attaching|enclosed|u prilogu|prilog|prilažem|prilazem|šaljem u prilogu|im anhang|angehängt|pièce jointe|ci-joint)\b/.test(text)
}
