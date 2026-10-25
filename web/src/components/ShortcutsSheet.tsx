import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Today'], [['b'], 'Breathe'], [['m'], 'Meditate'], [['p'], 'The Path'], [['j'], 'Journal'], [['s'], 'Statistics'], [['g'], 'Milestones'], [[','], 'Settings'], [['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Start', keys: [[['1'], 'A one-minute reset'], [['t'], 'A silent sit'], [['n'], 'Add a practice by hand']] },
  { title: 'During a practice', keys: [[['space'], 'Pause or go on'], [['esc'], 'End'], [['f'], 'Full screen']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
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
