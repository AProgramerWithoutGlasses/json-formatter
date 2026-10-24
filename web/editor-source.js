import { EditorState, Compartment, StateEffect, StateField, EditorSelection, Prec } from '@codemirror/state';
import { EditorView, Decoration, keymap, lineNumbers, placeholder } from '@codemirror/view';
import { json } from '@codemirror/lang-json';
import { codeFolding, foldService, syntaxTree, foldGutter, foldKeymap, foldAll, unfoldAll, bracketMatching, ensureSyntaxTree } from '@codemirror/language';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';

const repairEffect = StateEffect.define();
const repairField = StateField.define({
  create: () => Decoration.none,
  update(value, transaction) {
    value = value.map(transaction.changes);
    for (const effect of transaction.effects) {
      if (effect.is(repairEffect)) {
        value = Decoration.set(effect.value.filter((r) => r.start < r.end && r.end <= transaction.newDoc.length)
          .map((r) => Decoration.mark({ class: 'cm-repair' }).range(r.start, r.end)), true);
      }
    }
    return value;
  },
  provide: (field) => EditorView.decorations.from(field),
});

// A small adapter keeps formatting/repair/history independent of the editor UI.
// The document is always full text; folding never changes it or parses numbers.
class JSONEditor extends EventTarget {
  constructor(parent) {
    super();
    this.silent = false;
    this.wrapping = new Compartment();
    this.wrapValue = 'soft';
    this.view = new EditorView({
      parent,
      state: EditorState.create({
        extensions: [
          json(), lineNumbers(), foldGutter({ markerDOM: (open) => {
            const button = document.createElement('button');
            button.type = 'button'; button.className = 'fold-toggle';
            button.textContent = open ? '▾' : '▸';
            button.setAttribute('aria-expanded', String(open));
            button.setAttribute('aria-label', open ? '收起此段' : '展开此段');
            button.title = open ? '收起此段' : '展开此段';
            return button;
          } }), bracketMatching(), history(), repairField,
          foldService.of((state, lineStart) => {
            // The default service only offers multiline folds. Compact JSON
            // can still collapse its root container and report one hidden line.
            if (state.doc.lines !== 1 || lineStart !== 0) return null;
            const node = syntaxTree(state).topNode.firstChild;
            const close = node?.name === 'Object' ? '}' : node?.name === 'Array' ? ']' : null;
            if (!close || node.lastChild?.name !== close || node.to - node.from <= 2) return null;
            return { from: node.from + 1, to: node.to - 1 };
          }),
          codeFolding({
            preparePlaceholder: (state, { from, to }) => {
              const first = state.doc.lineAt(from), last = state.doc.lineAt(to);
              // Count hidden content lines, excluding brace-only boundary lines.
              // Soft wrapping and already-folded children do not affect the count.
              const start = first.number + (state.doc.sliceString(from, first.to).trim() ? 0 : 1);
              const end = last.number - (state.doc.sliceString(last.from, to).trim() ? 0 : 1);
              return Math.max(1, end - start + 1);
            },
            placeholderDOM: (_view, onclick, lines) => {
              const button = document.createElement('button');
              button.type = 'button'; button.className = 'cm-foldPlaceholder';
              button.textContent = `⋯ ${lines} 行`;
              button.title = `已省略 ${lines} 行内容，点击展开`;
              button.setAttribute('aria-label', button.title);
              button.onclick = onclick;
              return button;
            },
          }),
          EditorState.phrases.of({ 'folded code': '已收起的 JSON', unfold: '展开此段' }),
          this.wrapping.of(EditorView.lineWrapping),
          placeholder('粘贴 JSON，直接在这里整理\n\n例如：{"hello":"world"}'),
          EditorState.tabSize.of(2),
          EditorView.contentAttributes.of({ id: 'json-content', 'aria-label': 'JSON 编辑器', 'aria-describedby': 'input-hint repair-summary', spellcheck: 'false', autocapitalize: 'off' }),
          Prec.highest(keymap.of([{ key: 'Mod-Enter', run: () => {
            document.dispatchEvent(new Event('json-format-request'));
            return true;
          } }])),
          keymap.of([...defaultKeymap, ...historyKeymap, ...foldKeymap]),
          EditorView.domEventHandlers({
            compositionstart: () => { this.dispatchEvent(new Event('compositionstart')); },
            compositionend: () => { this.dispatchEvent(new Event('compositionend')); },
          }),
          EditorView.updateListener.of((update) => {
            if (!update.docChanged || this.silent) return;
            const event = new Event('input');
            event.inputType = update.transactions.some((t) => t.isUserEvent('input.paste')) ? 'insertFromPaste' : 'insertText';
            // Run after the editor update, so listeners can safely dispatch effects.
            queueMicrotask(() => this.dispatchEvent(event));
          }),
          EditorView.theme({
            '&': { height: '100%', fontSize: '14px', color: '#222', backgroundColor: '#fff' },
            '.cm-scroller': { fontFamily: 'Consolas, "SFMono-Regular", "Liberation Mono", monospace', lineHeight: '1.75', overflow: 'auto' },
            '.cm-content': { padding: '12px 0', minHeight: '100%' },
            '.cm-line': { padding: '0 10px' },
            '.cm-gutters': { backgroundColor: '#fafafa', color: '#888', borderRight: '1px solid #eee' },
            '.cm-foldGutter .cm-gutterElement': { cursor: 'pointer', minWidth: '22px', textAlign: 'center' },
            '.cm-foldPlaceholder': { backgroundColor: '#f1f3f5', color: '#555', border: '1px solid #ccc', padding: '0 6px', borderRadius: '3px', cursor: 'pointer', font: 'inherit', minHeight: '0', whiteSpace: 'nowrap' },
            '&.cm-focused': { outline: '2px solid #4778a8', outlineOffset: '-2px' },
            '.cm-repair': { backgroundColor: '#d9f5dc', color: '#14652a', textDecoration: 'underline' },
            '@media (max-width: 760px)': { '&': { fontSize: '16px' } },
          }),
        ],
      }),
    });
  }
  get value() { return this.view.state.doc.toString(); }
  set value(value) {
    if (value === this.value) return;
    this.silent = true;
    try { this.view.dispatch({ changes: { from: 0, to: this.view.state.doc.length, insert: value } }); }
    finally { this.silent = false; }
  }
  get selectionStart() { return this.view.state.selection.main.from; }
  get selectionEnd() { return this.view.state.selection.main.to; }
  get selectionDirection() { return this.view.state.selection.main.anchor > this.view.state.selection.main.head ? 'backward' : 'forward'; }
  setSelectionRange(start, end, direction = 'forward') {
    const length = this.view.state.doc.length;
    start = Math.min(start, length); end = Math.min(end, length);
    this.view.dispatch({ selection: EditorSelection.single(direction === 'backward' ? end : start, direction === 'backward' ? start : end) });
  }
  get scrollTop() { return this.view.scrollDOM.scrollTop; }
  set scrollTop(value) { this.view.scrollDOM.scrollTop = value; }
  get scrollLeft() { return this.view.scrollDOM.scrollLeft; }
  set scrollLeft(value) { this.view.scrollDOM.scrollLeft = value; }
  get wrap() { return this.wrapValue; }
  set wrap(value) { this.wrapValue = value; this.view.dispatch({ effects: this.wrapping.reconfigure(value === 'soft' ? EditorView.lineWrapping : []) }); }
  setAttribute(name, value) { this.view.contentDOM.setAttribute(name, value); }
  removeAttribute(name) { this.view.contentDOM.removeAttribute(name); }
  focus() { this.view.focus(); }
  setRepairRanges(ranges) { this.view.dispatch({ effects: repairEffect.of(ranges) }); }
  foldAll() { ensureSyntaxTree(this.view.state, this.view.state.doc.length, 200); foldAll(this.view); }
  unfoldAll() { unfoldAll(this.view); }
}

window.jsonEditor = new JSONEditor(document.getElementById('json-input'));
