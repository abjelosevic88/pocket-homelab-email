import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['g', 'i'], 'Inbox'], [['g', 's'], 'Starred'], [['g', 'd'], 'Drafts'], [['g', 't'], 'Sent'], [['g', 'a'], 'Archive'], [['/'], 'Search'], [['⌘', 'K'], 'Commands'], [[','], 'Settings']] },
  { title: 'Messages', keys: [[['j'], 'Next'], [['k'], 'Previous'], [['o'], 'Open'], [['u'], 'Back to the list'], [['x'], 'Select'], [['e'], 'Archive'], [['#'], 'Delete'], [['s'], 'Star'], [['⇧', 'I'], 'Mark as read'], [['⇧', 'U'], 'Mark as unread'], [['b'], 'Snooze'], [['v'], 'Move to a folder'], [['!'], 'Spam'], [['z'], 'Undo']] },
  { title: 'Writing', keys: [[['c'], 'New message'], [['r'], 'Reply'], [['a'], 'Reply all'], [['f'], 'Forward'], [['⌘', '↵'], 'Send'], [['esc'], 'Minimise the message']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['?'], 'This sheet'], [['esc'], 'Close a dialog or menu']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
    </Modal>
  )
}
