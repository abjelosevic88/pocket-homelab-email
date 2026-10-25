// Writing: windows docked at the bottom right (full screen on phones) that save as you type, with
// attachments, templates, AI help, undo send and send later.
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useMail } from '../lib/mail-context'
import { announceChange, fileIcon, loadApps, mentionsAttachment, scheduleChoices, useWindowEvent, validEmail, who, type ComposeOpen } from '../lib/mail'
import { fmtBytes, fmtDateTime } from '../lib/format'
import type { Draft, DraftFile, SendResult, Template } from '../lib/types'
import { Icon } from './icons'
import { Select } from './Select'
import { AddressField } from './AddressField'
import { RichEditor, cleanHTML, type EditorHandle } from './RichEditor'
import { AttachPicker, PickTimeDialog } from './dialogs'
import { Confirm, Field, Menu, type MenuItem, Modal, Spinner, rectOf, useIsMobile, useToast, type Anchor } from './ui'

type Win = { key: number; open: ComposeOpen; min: boolean }
let seq = 1

export function ComposerHost() {
  const [wins, setWins] = useState<Win[]>([])
  const mobile = useIsMobile()
  useWindowEvent<ComposeOpen>('pml:compose', open => {
    setWins(ws => {
      if ('draft_id' in open) {
        const i = ws.findIndex(w => w.open && 'draft_id' in w.open && w.open.draft_id === open.draft_id)
        if (i >= 0) return ws.map((w, j) => j === i ? { ...w, min: false } : w)
      }
      const rest = ws.map(w => ({ ...w, min: true }))
      return [...rest.slice(-2), { key: seq++, open, min: false }]
    })
  })
  const close = useCallback((key: number) => setWins(ws => ws.filter(w => w.key !== key)), [])
  const toggle = useCallback((key: number) => setWins(ws => ws.map(w => w.key === key ? { ...w, min: !w.min } : (mobile ? { ...w, min: true } : w))), [mobile])
  if (!wins.length) return null
  return (
    <div className="composer-dock">
      {wins.map(w => <Composer key={w.key} open={w.open} min={w.min} onToggle={() => toggle(w.key)} onClose={() => close(w.key)} />)}
    </div>
  )
}

const SIG_RE = /<div[^>]*(?:data-signature|class="signature")[^>]*>[\s\S]*?<\/div>/i

/** Split the body into what you wrote and the rest (signature, quote). */
function splitBody(html: string): [string, string] {
  const m = html.search(/<div[^>]*data-signature|<div[^>]*class="(signature|pml-quote)"|<blockquote|data-quote/i)
  return m < 0 ? [html, ''] : [html.slice(0, m), html.slice(m)]
}

function withSignature(html: string, sig: string): string {
  const block = sig.trim() ? `<div data-signature="1" class="signature">${sig}</div>` : ''
  if (SIG_RE.test(html)) return html.replace(SIG_RE, block)
  if (!block) return html
  const [own, rest] = splitBody(html)
  return `${own || '<p><br></p>'}<p><br></p>${block}${rest}`
}

const textOf = (html: string) => html.replace(/<[^>]+>/g, ' ').replace(/&nbsp;/g, ' ').trim()

function Composer({ open, min, onToggle, onClose }: { open: ComposeOpen; min: boolean; onToggle: () => void; onClose: () => void }) {
  const { settings } = useApp()
  const mail = useMail()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const [d, setD] = useState<Draft | null>(null)
  const [err, setErr] = useState('')
  const [big, setBig] = useState(false)
  const [showCc, setShowCc] = useState(false)
  const [saved, setSaved] = useState<'' | 'saving' | 'saved' | 'error'>('')
  const [uploading, setUploading] = useState(0)
  const [drag, setDrag] = useState(false)
  const [menu, setMenu] = useState<{ kind: 'schedule' | 'ai' | 'templates' | 'attach'; at: Anchor } | null>(null)
  const [pick, setPick] = useState<'time' | 'apps' | 'template' | null>(null)
  const [ask, setAsk] = useState<null | { title: string; message: string; go: () => void }>(null)
  const [discard, setDiscard] = useState(false)
  const [ai, setAI] = useState<{ mode: string; prompt: string; busy: boolean; result: string; subject?: string } | null>(null)
  const [aiReady, setAIReady] = useState(false)
  const [appsOK, setAppsOK] = useState(false)
  const editor = useRef<EditorHandle>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const dirty = useRef(false)
  const timer = useRef(0)
  const dRef = useRef<Draft | null>(null)
  dRef.current = d

  // ---- load or create the draft ----
  useEffect(() => {
    let live = true
    const sigOf = (id: number) => mail.account(id)?.signature || ''
    const go = async () => {
      try {
        let draft: Draft
        if ('draft_id' in open) draft = await api.get<Draft>(`${V1}/drafts/${open.draft_id}`)
        else {
          const req = { ...open }
          if (!req.account_id && settings?.default_account) req.account_id = settings.default_account
          draft = await api.post<Draft>(`${V1}/compose`, req)
          const sig = sigOf(draft.account_id)
          if (sig && !SIG_RE.test(draft.body_html)) {
            draft = { ...draft, body_html: withSignature(draft.body_html, sig) }
            dirty.current = true
          }
          if (open.body_html && !draft.body_html.includes(open.body_html.slice(0, 40))) {
            const [own, rest] = splitBody(draft.body_html)
            draft = { ...draft, body_html: (textOf(own) ? own : '') + open.body_html + rest }
            dirty.current = true
          }
        }
        if (!live) return
        setD(draft)
        if (draft.cc.length || draft.bcc.length) setShowCc(true)
        announceChange()
      } catch (e) { if (live) setErr((e as Error).message) }
    }
    go()
    loadApps().then(a => { setAIReady(a.ai.ready); setAppsOK(!!(a.apps.files?.configured || a.apps.documents?.configured)) })
    return () => { live = false }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ---- autosave ----
  const save = useCallback(async () => {
    const cur = dRef.current
    if (!cur || !dirty.current || cur.status !== 'draft' && cur.status !== 'failed') return
    dirty.current = false
    setSaved('saving')
    try {
      const r = await api.put<Draft>(`${V1}/drafts/${cur.id}`, { account_id: cur.account_id, to: cur.to, cc: cur.cc, bcc: cur.bcc, subject: cur.subject, body_html: cur.body_html })
      setD(x => x ? { ...x, updated_at: r.updated_at } : x)
      setSaved('saved')
    } catch { dirty.current = true; setSaved('error') }
  }, [])
  const change = (patch: Partial<Draft>) => {
    setD(x => x ? { ...x, ...patch } : x)
    dirty.current = true
    setSaved('')
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(save, 900)
  }
  useEffect(() => () => { window.clearTimeout(timer.current) }, [])
  useEffect(() => { if (d && dirty.current) { timer.current = window.setTimeout(save, 900) } }, [d?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  const setAccount = (id: number) => {
    if (!d) return
    const sig = mail.account(id)?.signature || ''
    const body = withSignature(d.body_html, sig)
    change({ account_id: id, body_html: body })
    editor.current?.setHTML(body)
  }

  // ---- files ----
  const addFiles = async (files: File[]) => {
    if (!d) return
    setUploading(n => n + files.length)
    for (const f of files) {
      try {
        const df = await upload<DraftFile>(`${V1}/drafts/${d.id}/files`, 'file', f)
        setD(x => x ? { ...x, files: [...x.files, df] } : x)
      } catch (e) { toast(`${f.name}: ${(e as Error).message}`, 'err') } finally { setUploading(n => n - 1) }
    }
  }
  const dropFile = async (f: DraftFile) => {
    if (!d) return
    try { await api.del(`${V1}/drafts/${d.id}/files/${f.id}`); setD(x => x ? { ...x, files: x.files.filter(y => y.id !== f.id) } : x) } catch (e) { toast((e as Error).message, 'err') }
  }
  const reloadFiles = async () => { if (!d) return; try { const r = await api.get<Draft>(`${V1}/drafts/${d.id}`); setD(x => x ? { ...x, files: r.files } : x) } catch { /* keep */ } }

  // ---- send ----
  const doSend = async (sendAt?: string) => {
    if (!d) return
    window.clearTimeout(timer.current)
    dirty.current = true
    await save()
    try {
      const r = await api.post<SendResult>(`${V1}/drafts/${d.id}/send`, sendAt ? { send_at: sendAt } : {})
      onClose()
      announceChange()
      const id = d.id
      const undo = async () => {
        try { await api.post(`${V1}/drafts/${id}/cancel`); announceChange(); window.dispatchEvent(new CustomEvent('pml:compose', { detail: { draft_id: id } })) }
        catch { toast('Too late: it is already on its way.', 'err') }
      }
      if (sendAt) toast(`Scheduled for ${fmtDateTime(r.draft.send_at || sendAt)}`, 'ok', { label: 'Undo', run: undo })
      else if ((settings?.undo_send_seconds ?? 10) > 0 && r.undo_until && new Date(r.undo_until) > new Date()) toast('Sending…', 'ok', { label: 'Undo', run: undo })
      else toast('Sent')
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const send = (sendAt?: string) => {
    if (!d) return
    const all = [...d.to, ...d.cc, ...d.bcc]
    if (!all.length) { toast('Add at least one recipient.', 'err'); return }
    const bad = all.find(a => !validEmail(a.addr))
    if (bad) { toast(`“${bad.addr}” is not an email address.`, 'err'); return }
    const checks: { title: string; message: string }[] = []
    if (!d.subject.trim()) checks.push({ title: 'Send without a subject?', message: 'The message has no subject line.' })
    if (!d.files.length && mentionsAttachment(d.body_html)) checks.push({ title: 'Forgot the attachment?', message: 'The text mentions an attachment, but nothing is attached.' })
    const next = (i: number) => {
      if (i >= checks.length) { setAsk(null); doSend(sendAt); return }
      setAsk({ ...checks[i], go: () => next(i + 1) })
    }
    next(0)
  }

  const closeWin = async () => {
    window.clearTimeout(timer.current)
    if (d && !d.to.length && !d.cc.length && !d.subject.trim() && !textOf(splitBody(d.body_html)[0]) && !d.files.length && d.kind === 'new') {
      await api.del(`${V1}/drafts/${d.id}`).catch(() => {})
      announceChange()
    } else {
      await save()
      if (d) toast('Saved to Drafts')
      announceChange()
    }
    onClose()
  }
  const doDiscard = async () => {
    setDiscard(false)
    window.clearTimeout(timer.current)
    if (d) await api.del(`${V1}/drafts/${d.id}`).catch(() => {})
    announceChange()
    onClose()
    toast('Draft deleted')
  }

  // ---- AI ----
  const runAI = async (mode: string, prompt = '') => {
    if (!d) return
    const [own] = splitBody(d.body_html)
    setAI({ mode, prompt, busy: true, result: '' })
    try {
      const r = await api.post<{ body_html: string; subject?: string }>(`${V1}/ai/write`, { mode, text_html: own, prompt, language: mode === 'translate' ? prompt : undefined, message_id: d.reply_to_id || undefined })
      setAI({ mode, prompt, busy: false, result: cleanHTML(r.body_html), subject: r.subject })
    } catch (e) { toast((e as Error).message, 'err'); setAI(null) }
  }
  const applyAI = (how: 'replace' | 'insert') => {
    if (!d || !ai?.result) return
    const [own, rest] = splitBody(d.body_html)
    const body = how === 'replace' || !textOf(own) ? ai.result + rest : own + ai.result + rest
    change({ body_html: body, ...(ai.subject && !d.subject ? { subject: ai.subject } : {}) })
    editor.current?.setHTML(body)
    setAI(null)
  }

  // ---- templates ----
  const [templates, setTemplates] = useState<Template[] | null>(null)
  const [tplName, setTplName] = useState('')
  const openTemplates = async (at: Anchor) => {
    setMenu({ kind: 'templates', at })
    if (!templates) api.get<Template[]>(`${V1}/templates`).then(setTemplates).catch(() => setTemplates([]))
  }
  const saveTemplate = async () => {
    if (!d || !tplName.trim()) return
    try { const t = await api.post<Template>(`${V1}/templates`, { name: tplName.trim(), subject: d.subject, body_html: splitBody(d.body_html)[0] }); setTemplates(x => [...(x || []), t]); toast('Template saved'); setPick(null); setTplName('') } catch (e) { toast((e as Error).message, 'err') }
  }

  const accounts = mail.accounts
  const title = d ? (d.subject || (d.kind === 'forward' ? 'Forward' : d.kind.startsWith('reply') ? 'Reply' : 'New message')) : 'New message'
  if (min && !mobile) return (
    <div className="composer min" onClick={onToggle} role="button" tabIndex={0}>
      <Icon name="edit" size={14} /><span className="grow ellipsis">{title}</span>
      <button type="button" className="btn ghost icon-only sm" aria-label="Close" onClick={e => { e.stopPropagation(); closeWin() }}><Icon name="x" size={14} /></button>
    </div>
  )
  if (min) return null
  const menuItems = (): MenuItem[] => {
    if (!menu) return []
    if (menu.kind === 'schedule') return [{ heading: 'Send later' }, ...scheduleChoices().map(c => ({ label: c.label, hint: c.hint, icon: 'clock', onSelect: () => send(c.when.toISOString()) })), 'sep', { label: 'Pick date & time…', icon: 'calendar', onSelect: () => setPick('time') }]
    if (menu.kind === 'ai') {
      const dis = !aiReady
      return [
        { heading: aiReady ? 'Write with AI' : 'Connect AI in Settings → AI' },
        { label: 'Write it for me…', icon: 'sparkles', disabled: dis, onSelect: () => setAI({ mode: 'write', prompt: '', busy: false, result: '' }) },
        { label: 'Improve', icon: 'edit', disabled: dis, onSelect: () => runAI('improve') },
        { label: 'Make it shorter', icon: 'minus', disabled: dis, onSelect: () => runAI('shorten') },
        { label: 'Friendlier', icon: 'smile', disabled: dis, onSelect: () => runAI('friendly') },
        { label: 'More formal', icon: 'user', disabled: dis, onSelect: () => runAI('formal') },
        { label: 'Fix spelling and grammar', icon: 'check', disabled: dis, onSelect: () => runAI('fix') },
        { label: 'Translate…', icon: 'globe', disabled: dis, onSelect: () => setAI({ mode: 'translate', prompt: 'English', busy: false, result: '' }) },
      ]
    }
    if (menu.kind === 'attach') return [
      { label: 'From this device…', icon: 'upload', onSelect: () => fileInput.current?.click() },
      { label: 'From Pocket Files or Documents…', icon: 'folder', disabled: !appsOK, hint: appsOK ? undefined : 'not set up', onSelect: () => setPick('apps') },
    ]
    return [
      { heading: 'Templates' },
      ...(templates === null ? [{ label: 'Loading…', disabled: true }] : templates.length === 0 ? [{ label: 'No templates yet', disabled: true }] : templates.map(t => ({ label: t.name, icon: 'file-text', onSelect: () => {
        if (!d) return
        editor.current?.insertHTML(t.body_html)
        if (!d.subject && t.subject) change({ subject: t.subject })
      } }))),
      'sep',
      { label: 'Save this as a template…', icon: 'plus', onSelect: () => { setTplName(d?.subject || ''); setPick('template') } },
    ]
  }
  return (
    <div className={`composer ${big ? 'big' : ''} ${drag ? 'drag' : ''}`} role="dialog" aria-label={title}
      onDragOver={e => { if (e.dataTransfer.types.includes('Files')) { e.preventDefault(); setDrag(true) } }}
      onDragLeave={e => { if (e.currentTarget === e.target) setDrag(false) }}
      onDrop={e => { e.preventDefault(); setDrag(false); if (e.dataTransfer.files.length) addFiles([...e.dataTransfer.files]) }}
      onKeyDown={e => {
        if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); send() }
        else if (e.key === 'Escape' && !document.querySelector('.menu, .modal-backdrop, .addr-pop')) { e.preventDefault(); if (mobile) closeWin(); else onToggle() }
      }}>
      <div className="cmp-head" onClick={() => !mobile && onToggle()}>
        {mobile && <button type="button" className="btn ghost icon-only" aria-label="Close" onClick={e => { e.stopPropagation(); closeWin() }}><Icon name="arrow-left" size={18} /></button>}
        <span className="cmp-title ellipsis">{title}</span>
        <span className="cmp-saved muted xsmall">{saved === 'saving' ? 'Saving…' : saved === 'saved' ? 'Saved' : saved === 'error' ? 'Not saved' : ''}</span>
        {!mobile && <>
          <button type="button" className="btn ghost icon-only sm" aria-label="Minimise" title="Minimise (esc)" onClick={e => { e.stopPropagation(); onToggle() }}><Icon name="minus" size={14} /></button>
          <button type="button" className="btn ghost icon-only sm" aria-label={big ? 'Smaller' : 'Bigger'} title={big ? 'Smaller' : 'Bigger'} onClick={e => { e.stopPropagation(); setBig(b => !b) }}><Icon name="maximize" size={13} /></button>
          <button type="button" className="btn ghost icon-only sm" aria-label="Close" title="Close (saved to Drafts)" onClick={e => { e.stopPropagation(); closeWin() }}><Icon name="x" size={14} /></button>
        </>}
        {mobile && d && <button type="button" className="btn primary sm" onClick={e => { e.stopPropagation(); send() }}><Icon name="send" size={14} />Send</button>}
      </div>
      {err ? <div className="cmp-body"><div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div></div>
        : !d ? <div className="cmp-body"><div className="loading"><Spinner /> Preparing…</div></div>
        : <>
          <div className="cmp-fields">
            {accounts.length > 1 && <div className="addr-field from-field"><span className="addr-label">From</span>
              <Select ariaLabel="From" className="from-select" value={String(d.account_id)} onChange={v => setAccount(Number(v))}
                options={accounts.map(a => ({ value: String(a.id), label: a.name ? `${a.name} <${a.email}>` : a.email, hint: a.label || undefined }))} />
            </div>}
            {accounts.length === 0 && <div className="callout warn small"><Icon name="alert" size={14} /><span>Add a mail account first (Settings → Accounts).</span></div>}
            <AddressField label="To" value={d.to} onChange={v => change({ to: v })} autoFocus={!d.to.length}
              after={!showCc && <button type="button" className="link-btn small cc-toggle" onClick={e => { e.stopPropagation(); setShowCc(true) }}>Cc Bcc</button>} />
            {showCc && <AddressField label="Cc" value={d.cc} onChange={v => change({ cc: v })} />}
            {showCc && <AddressField label="Bcc" value={d.bcc} onChange={v => change({ bcc: v })} />}
            <div className="addr-field"><input className="subject-in" value={d.subject} onChange={e => change({ subject: e.target.value })} placeholder="Subject" aria-label="Subject" autoFocus={!!d.to.length && !d.subject} /></div>
          </div>
          <div className="cmp-body">
            <RichEditor ref={editor} value={d.body_html} onChange={h => change({ body_html: h })} onFiles={addFiles} onSubmit={() => send()} placeholder="Write your message" minHeight={big ? 320 : 180} />
            {ai && <div className="ai-panel">
              {ai.result ? <>
                <div className="ai-h"><Icon name="sparkles" size={13} /> Suggestion</div>
                <div className="ai-result" dangerouslySetInnerHTML={{ __html: ai.result }} />
                <div className="row wrap">
                  <button type="button" className="btn primary sm" onClick={() => applyAI('replace')}>Replace my text</button>
                  <button type="button" className="btn sm" onClick={() => applyAI('insert')}>Insert</button>
                  <button type="button" className="btn ghost sm" onClick={() => runAI(ai.mode, ai.prompt)}>Try again</button>
                  <button type="button" className="btn ghost sm" onClick={() => setAI(null)}>Discard</button>
                </div>
              </> : ai.busy ? <div className="loading"><Spinner /> Writing…</div> : <form className="ai-ask" onSubmit={e => { e.preventDefault(); runAI(ai.mode, ai.prompt) }}>
                <input autoFocus value={ai.prompt} onChange={e => setAI({ ...ai, prompt: e.target.value })} aria-label={ai.mode === 'translate' ? 'Language' : 'What should it say'}
                  placeholder={ai.mode === 'translate' ? 'Into which language? (English, Srpski, Deutsch…)' : 'e.g. thank them for the offer and ask for a call next week'} />
                <button className="btn primary sm" disabled={!ai.prompt.trim()}><Icon name="sparkles" size={13} />{ai.mode === 'translate' ? 'Translate' : 'Write'}</button>
                <button type="button" className="btn ghost sm" onClick={() => setAI(null)}>Cancel</button>
              </form>}
            </div>}
            {(d.files.length > 0 || uploading > 0) && <div className="cmp-files">
              {d.files.map(f => (
                <span key={f.id} className="cmp-file" title={f.filename}>
                  <Icon name={fileIcon(f.mime, f.filename)} size={14} />
                  <a className="ellipsis" href={`${V1}/drafts/${d.id}/files/${f.id}`} target="_blank" rel="noreferrer">{f.filename}</a>
                  <span className="muted xsmall">{fmtBytes(f.size)}</span>
                  <button type="button" aria-label={`Remove ${f.filename}`} onClick={() => dropFile(f)}><Icon name="x" size={12} /></button>
                </span>
              ))}
              {uploading > 0 && <span className="cmp-file"><Spinner size={12} />Adding {uploading}…</span>}
            </div>}
            {d.status === 'failed' && d.error && <div className="callout danger small"><Icon name="alert" size={14} /><span>Last try failed: {d.error}</span></div>}
          </div>
          <div className="cmp-foot">
            <div className="send-group">
              <button type="button" className="btn primary" onClick={() => send()} title="Send (⌘↵)"><Icon name="send" size={15} />Send</button>
              <button type="button" className="btn primary send-more" aria-label="Send later" title="Send later" onClick={e => setMenu({ kind: 'schedule', at: rectOf(e.currentTarget) })}><Icon name="chevron-down" size={14} /></button>
            </div>
            <button type="button" className="btn ghost icon-only" aria-label="Attach" title="Attach files" onClick={e => setMenu({ kind: 'attach', at: rectOf(e.currentTarget) })}><Icon name="paperclip" size={17} /></button>
            <button type="button" className={`btn ghost icon-only ${aiReady ? '' : 'dim'}`} aria-label="Write with AI" title={aiReady ? 'Write with AI' : 'Connect AI in Settings → AI'} onClick={e => setMenu({ kind: 'ai', at: rectOf(e.currentTarget) })}><Icon name="sparkles" size={17} /></button>
            <button type="button" className="btn ghost icon-only" aria-label="Templates" title="Templates" onClick={e => openTemplates(rectOf(e.currentTarget))}><Icon name="file-text" size={17} /></button>
            <span className="spacer" />
            {d.to.length > 0 && !mobile && <span className="muted xsmall ellipsis cmp-to">to {d.to.slice(0, 2).map(who).join(', ')}{d.to.length > 2 ? ` +${d.to.length - 2}` : ''}</span>}
            <button type="button" className="btn ghost icon-only" aria-label="Delete draft" title="Delete draft" onClick={() => setDiscard(true)}><Icon name="trash" size={16} /></button>
          </div>
          <input ref={fileInput} type="file" multiple hidden onChange={e => { if (e.target.files) addFiles([...e.target.files]); e.target.value = '' }} />
        </>}
      {drag && <div className="drop-hint"><Icon name="paperclip" size={22} />Drop to attach</div>}
      {menu && <Menu anchor={menu.at} align={menu.kind === 'schedule' ? 'left' : 'left'} width={260} items={menuItems()} onClose={() => setMenu(null)} />}
      {pick === 'time' && <PickTimeDialog title="Send later" confirm="Schedule" onClose={() => setPick(null)} onPick={iso => { setPick(null); send(iso) }} />}
      {pick === 'apps' && d && <AttachPicker draftId={d.id} onClose={() => setPick(null)} onAdded={() => { reloadFiles() }} />}
      {pick === 'template' && <Modal title="Save as a template" size="sm" onClose={() => setPick(null)} footer={<><button type="button" className="btn" onClick={() => setPick(null)}>Cancel</button><button type="button" className="btn primary" disabled={!tplName.trim()} onClick={saveTemplate}>Save</button></>}>
        <Field label="Name" help="Your text (without the signature and quote) and the subject are saved."><input value={tplName} onChange={e => setTplName(e.target.value)} autoFocus /></Field>
      </Modal>}
      {ask && <Confirm title={ask.title} message={ask.message} danger={false} confirmLabel="Send anyway" onCancel={() => setAsk(null)} onConfirm={ask.go} />}
      {discard && <Confirm title="Delete this draft?" message="What you wrote is gone for good." confirmLabel="Delete" onCancel={() => setDiscard(false)} onConfirm={doDiscard} />}
    </div>
  )
}
