// Der Editor haelt genau ein Markdown-Dokument als Folge von Blockelementen
// im DOM. Bloecke, die niemand angefasst hat, wandern beim Speichern
// unveraendert aus ihren Quellzeilen zurueck in die Datei.

import * as md from './md.js';

let uid = 1;

const TEXT = Node.TEXT_NODE;
const ELEM = Node.ELEMENT_NODE;

// ------------------------------------------------------------ Cursor

function pointAt(el, pos) {
  const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  let n, seen = 0;
  while ((n = walk.nextNode())) {
    if (seen + n.data.length >= pos) return { node: n, offset: pos - seen };
    seen += n.data.length;
  }
  return { node: el, offset: pos <= 0 ? 0 : el.childNodes.length };
}

// ensureFillable haelt einen leeren Block bewohnbar: einen leeren Textknoten
// raeumt der Browser irgendwann weg und nimmt die Auswahl gleich mit.
function ensureFillable(el) {
  if (el.textContent === '' && !el.querySelector('br, img, td, th')) {
    el.replaceChildren(document.createElement('br'));
  }
}

function setCaret(el, pos) {
  const p = pointAt(el, Math.max(0, pos));
  const r = document.createRange();
  try { r.setStart(p.node, Math.min(p.offset, p.node.length ?? p.node.childNodes.length)); } catch { r.selectNodeContents(el); }
  r.collapse(true);
  const s = getSelection();
  s.removeAllRanges();
  s.addRange(r);
}

function caretOffset(el) {
  const s = getSelection();
  if (!s.rangeCount) return -1;
  const r = s.getRangeAt(0);
  if (!el.contains(r.startContainer)) return -1;
  const pre = document.createRange();
  pre.selectNodeContents(el);
  pre.setEnd(r.startContainer, r.startOffset);
  return pre.toString().length;
}

const isEmptyEl = (el) => el.textContent.replace(/[\u00A0\u200B]/g, ' ').trim() === '' && !el.querySelector('img, table');

// ------------------------------------------------------------ Editor

export class Editor {
  constructor(root, opts = {}) {
    this.root = root;
    this.onChange = opts.onChange || (() => {});
    this.ctx = {};
    this.blocks = new Map();
    this.step = 2;
    this.suspend = 0;
    this.history = [];
    this.future = [];
    this.snapTimer = 0;

    root.setAttribute('contenteditable', 'true');
    this.mo = new MutationObserver((muts) => this.onMutate(muts));

    root.addEventListener('keydown', (e) => this.onKeyDown(e));
    root.addEventListener('beforeinput', (e) => this.onBeforeInput(e));
    root.addEventListener('paste', (e) => this.onPaste(e));
    root.addEventListener('click', (e) => this.onClick(e));
  }

  // --------------------------------------------------------- Aufbau

  load(text, ctx) {
    this.ctx = ctx || this.ctx;
    this.render(text);
    this.history = [{ text: this.serialize().text, caret: { i: 0, off: 0 } }];
    this.future = [];
  }

  render(text) {
    this.suspend++;
    this.mo.disconnect();

    const { blocks, step } = md.parse(text);
    this.step = step;
    this.blocks.clear();
    this.root.replaceChildren();
    for (const b of blocks) this.root.appendChild(this.makeEl(b));
    if (!this.root.children.length) {
      this.root.appendChild(this.makeEl({ type: 'p', text: '', lead: 0, trail: 0, dirty: false, src: [''] }));
    }
    this.renumber();

    this.mo.observe(this.root, { subtree: true, childList: true, characterData: true });
    this.suspend--;
  }

  makeEl(b) {
    b.id = uid++;
    if (b.trail === undefined) b.trail = 1;
    if (b.lead === undefined) b.lead = 0;
    if (b.dirty === undefined) b.dirty = true;
    this.blocks.set(b.id, b);
    const el = this.buildEl(b);
    el.dataset.bid = String(b.id);
    return el;
  }

  buildEl(b) {
    const html = (t) => md.inlineToHTML(t || '', this.ctx) || '<br>';
    let el;
    switch (b.type) {
      case 'h':
        el = document.createElement('h' + Math.min(6, Math.max(1, b.level)));
        el.innerHTML = html(b.text);
        break;
      case 'quote':
        el = document.createElement('blockquote');
        el.dataset.level = String(Math.min(4, Math.max(1, b.level || 1)));
        el.innerHTML = html(b.text);
        break;
      case 'li':
        el = document.createElement('div');
        el.className = 'li';
        if (b.ordered) { el.dataset.ordered = '1'; el.dataset.num = String(b.num || 1); }
        if (b.delim) el.dataset.delim = b.delim;
        if (b.check !== null && b.check !== undefined) el.dataset.check = b.check ? '1' : '0';
        this.setIndentAttrs(el, b.indent || 0);
        el.innerHTML = html(b.text);
        break;
      case 'code':
        el = document.createElement('pre');
        el.className = 'code';
        el.dataset.lang = b.lang || '';
        el.textContent = b.lines.join('\n');
        break;
      case 'front':
        el = document.createElement('pre');
        el.className = 'front';
        el.textContent = b.lines.join('\n');
        break;
      case 'html':
        el = document.createElement('pre');
        el.className = 'raw';
        el.textContent = b.lines.join('\n');
        break;
      case 'hr':
        el = document.createElement('div');
        el.className = 'rule';
        el.setAttribute('contenteditable', 'false');
        break;
      case 'table':
        el = this.buildTable(b);
        break;
      default:
        el = document.createElement('p');
        el.innerHTML = html(b.text);
    }
    return el;
  }

  buildTable(b) {
    const table = document.createElement('table');
    const head = document.createElement('thead');
    const hr = document.createElement('tr');
    (b.rows[0] || []).forEach((c, i) => {
      const th = document.createElement('th');
      th.innerHTML = md.inlineToHTML(c, this.ctx) || '<br>';
      if (b.align[i]) { th.dataset.align = b.align[i]; th.style.textAlign = b.align[i]; }
      hr.appendChild(th);
    });
    head.appendChild(hr);
    table.appendChild(head);

    const body = document.createElement('tbody');
    for (const row of b.rows.slice(1)) {
      const tr = document.createElement('tr');
      row.forEach((c, i) => {
        const td = document.createElement('td');
        td.innerHTML = md.inlineToHTML(c, this.ctx) || '<br>';
        if (b.align[i]) td.style.textAlign = b.align[i];
        tr.appendChild(td);
      });
      body.appendChild(tr);
    }
    table.appendChild(body);
    return table;
  }

  setIndentAttrs(el, n) {
    el.dataset.indent = String(n);
    el.style.setProperty('--indent', String(n));
  }

  // ------------------------------------------------------ Serialisieren

  blockOf(el) { return this.blocks.get(+el.dataset.bid); }

  markDirty(el) {
    const b = this.blockOf(el);
    if (b) b.dirty = true;
  }

  // serialize liefert den sauberen Markdown-Text und fuer jeden Block den
  // Zeilenbereich, den er darin belegt — die Basis fuer die Randmarken.
  serialize() {
    const lines = [];
    const ranges = [];

    for (const el of this.root.children) {
      const b = this.blockOf(el);
      const lead = b ? b.lead || 0 : 0;
      const at = lines.length;

      if (b && !b.dirty) {
        lines.push(...b.src);
      } else {
        const nb = this.readEl(el);
        nb.lead = lead;
        nb.step = this.step;
        lines.push(...md.blockToLines(nb));
      }
      ranges.push({ el, start: at + lead, end: lines.length });

      const trail = b && b.trail !== undefined ? b.trail : 1;
      for (let i = 0; i < trail; i++) lines.push('');
    }

    while (lines.length && lines[lines.length - 1].trim() === '') lines.pop();
    return { text: lines.length ? lines.join('\n') + '\n' : '', ranges };
  }

  readEl(el) {
    const tag = el.tagName;
    if (/^H[1-6]$/.test(tag)) return { type: 'h', level: +tag[1], text: md.blockText(el) };
    if (tag === 'BLOCKQUOTE') return { type: 'quote', level: +(el.dataset.level || 1), text: md.blockText(el) };
    if (tag === 'PRE') {
      const lines = el.textContent.replace(/\u00A0/g, ' ').replace(/\n$/, '').split('\n');
      if (el.classList.contains('front')) return { type: 'front', lines };
      if (el.classList.contains('raw')) return { type: 'html', lines };
      return { type: 'code', lang: el.dataset.lang || '', lines };
    }
    if (tag === 'TABLE') {
      const rows = [...el.querySelectorAll('tr')].map((tr) => [...tr.children].map((c) => md.blockText(c)));
      const first = el.querySelector('tr');
      const align = first ? [...first.children].map((c) => c.dataset.align || '') : [];
      return { type: 'table', rows: rows.length ? rows : [['']], align };
    }
    if (el.classList.contains('rule')) return { type: 'hr' };
    if (el.classList.contains('li')) {
      return {
        type: 'li',
        ordered: el.dataset.ordered === '1',
        num: +(el.dataset.num || 1),
        delim: el.dataset.delim || (el.dataset.ordered === '1' ? '.' : '-'),
        indent: +(el.dataset.indent || 0),
        check: el.hasAttribute('data-check') ? el.dataset.check === '1' : null,
        text: md.blockText(el),
      };
    }
    return { type: 'p', text: md.blockText(el) };
  }

  renumber() {
    const counters = [];
    for (const el of this.root.children) {
      if (!el.classList.contains('li')) { counters.length = 0; continue; }
      const ind = +(el.dataset.indent || 0);
      counters.length = ind + 1;
      if (el.dataset.ordered === '1') {
        counters[ind] = (counters[ind] || 0) + 1;
        el.dataset.num = String(counters[ind]);
      } else {
        counters[ind] = 0;
      }
    }
  }

  // ---------------------------------------------------------- Randmarken

  applyRegions(regions) {
    const { ranges } = this.serialize();
    const before = new Set([...this.root.children].filter((el) => el.classList.contains('ann')));

    for (const el of this.root.children) {
      el.classList.remove('ann', 'ann-head', 'ann-cont', 'ann-gap', 'ann-new');
      delete el.dataset.tag;
      el.removeAttribute('title');
    }

    for (const r of regions || []) {
      if (r.start >= r.end) {
        const hit = ranges.find((x) => x.start >= r.start) || ranges[ranges.length - 1];
        if (hit) {
          hit.el.classList.add('ann-gap');
          hit.el.title = 'Hier wurde etwas entfernt — ' + tagFor(r).replace('\n', ', ');
        }
        continue;
      }
      const inside = ranges.filter((x) => x.start < r.end && x.end > r.start);
      inside.forEach((x, k) => {
        x.el.classList.add('ann');
        if (k === 0) { x.el.classList.add('ann-head'); x.el.dataset.tag = tagFor(r); }
        else x.el.classList.add('ann-cont');
        if (!before.has(x.el)) x.el.classList.add('ann-new');
      });
    }
  }

  // ------------------------------------------------------------ Ereignisse

  onMutate(muts) {
    if (this.suspend) return;
    for (const m of muts) {
      this.touch(m.target);
      m.addedNodes.forEach((n) => this.touch(n));
    }
    this.touchDoc();
  }

  touch(node) {
    let el = node.nodeType === ELEM ? node : node.parentElement;
    while (el && el.parentElement && el.parentElement !== this.root) el = el.parentElement;
    if (!el || el.parentElement !== this.root) return;
    this.markDirty(el);
  }

  touchDoc() {
    if (!this.root.children.length) {
      this.suspend++;
      this.root.appendChild(this.makeEl({ type: 'p', text: '', trail: 0 }));
      this.suspend--;
    }
    clearTimeout(this.snapTimer);
    this.snapTimer = setTimeout(() => this.snapshot(), 600);
    this.onChange();
  }

  onClick(e) {
    // Auf einen Aufgabenhaken klicken schaltet ihn um.
    const li = e.target.closest?.('.li[data-check]');
    if (!li || !this.root.contains(li)) return;
    // Nur der Markerbereich links vom Text schaltet um.
    const r = li.getBoundingClientRect();
    const pad = parseFloat(getComputedStyle(li).paddingLeft) || 26;
    if (e.clientX > r.left + pad) return;
    this.snapshot();
    li.dataset.check = li.dataset.check === '1' ? '0' : '1';
    this.markDirty(li);
    this.touchDoc();
  }

  onKeyDown(e) {
    const mod = e.metaKey || e.ctrlKey;
    const key = e.key.toLowerCase();

    if (mod && key === 'z') { e.preventDefault(); e.shiftKey ? this.redo() : this.undo(); return; }
    if (mod && key === 'y') { e.preventDefault(); this.redo(); return; }
    if (mod && key === 'b') { e.preventDefault(); document.execCommand('bold'); return; }
    if (mod && key === 'i') { e.preventDefault(); document.execCommand('italic'); return; }
    if (mod && key === 'k') { e.preventDefault(); this.linkSelection(); return; }
    if (mod) return;

    if (e.key === 'Enter') this.onEnter(e);
    else if (e.key === 'Backspace') this.onBackspace(e);
    else if (e.key === 'Delete') this.onDelete(e);
    else if (e.key === 'Tab') this.onTab(e);
  }

  blockElOf(node) {
    let el = node && node.nodeType === ELEM ? node : node?.parentElement;
    while (el && el.parentElement && el.parentElement !== this.root) el = el.parentElement;
    return el && el.parentElement === this.root ? el : null;
  }

  currentEl() {
    const s = getSelection();
    return s.rangeCount ? this.blockElOf(s.focusNode) : null;
  }

  onEnter(e) {
    const el = this.currentEl();
    if (!el) return;

    if (el.tagName === 'PRE') {
      e.preventDefault();
      const text = el.textContent;
      const pos = Math.max(0, caretOffset(el));

      // Zweimal Enter am Ende — oder Umschalt-Enter — verlaesst den Block.
      if (e.shiftKey || (pos >= text.length && text.endsWith('\n'))) {
        this.snapshot();
        if (!e.shiftKey) {
          el.textContent = text.replace(/\n$/, '');
          this.markDirty(el);
        }
        this.insertAfter(el, { type: 'p', text: '' });
        return;
      }
      // Den Inhalt bewusst als einen Textknoten fuehren: dann stimmen
      // Zeichenpositionen und Cursor auch nach vielen Zeilenumbruechen.
      el.textContent = text.slice(0, pos) + '\n' + text.slice(pos);
      this.markDirty(el);
      setCaret(el, pos + 1);
      this.touchDoc();
      return;
    }

    if (el.tagName === 'TABLE') return; // Zeilenumbruch in Zellen ueberlassen wir dem Browser

    if (e.shiftKey) {
      e.preventDefault();
      document.execCommand('insertLineBreak');
      return;
    }

    e.preventDefault();
    this.snapshot();

    // ```sprache und Enter oeffnet einen Codeblock.
    const fence = /^```([\w+#.-]*)$/.exec(el.textContent.replace(/\u00A0/g, ' ').trim());
    if (el.tagName === 'P' && fence) {
      const pre = this.makeEl({ type: 'code', lang: fence[1], lines: [''], trail: 1 });
      el.replaceWith(pre);
      setCaret(pre, 0);
      this.touchDoc();
      return;
    }

    // Ein Absatz aus drei Strichen wird zur Trennlinie.
    if (el.tagName === 'P' && /^(-{3,}|\*{3,}|_{3,})$/.test(el.textContent.trim())) {
      const hr = this.makeEl({ type: 'hr', trail: 1 });
      el.replaceWith(hr);
      this.insertAfter(hr, { type: 'p', text: '' });
      return;
    }

    const empty = isEmptyEl(el);
    if (empty && (el.classList.contains('li') || el.tagName === 'BLOCKQUOTE')) {
      // Leeres Listenelement oder leeres Zitat: eine Ebene heraus.
      if (el.classList.contains('li') && +(el.dataset.indent || 0) > 0) {
        this.setIndentAttrs(el, +(el.dataset.indent || 0) - 1);
        this.markDirty(el);
        this.renumber();
        this.touchDoc();
        return;
      }
      if (el.tagName === 'BLOCKQUOTE' && +(el.dataset.level || 1) > 1) {
        el.dataset.level = String(+(el.dataset.level || 1) - 1);
        this.markDirty(el);
        this.touchDoc();
        return;
      }
      this.morph(el, { type: 'p' });
      return;
    }

    const frag = this.splitTail(el);
    const spec = this.followerSpec(el);
    const b = this.blockOf(el);
    const oldTrail = b && b.trail !== undefined ? b.trail : 1;
    if (b) b.trail = spec.type === 'li' ? 0 : 1;

    const nel = this.makeEl({ ...spec, trail: oldTrail, lead: 0 });
    nel.replaceChildren();
    if (frag && (frag.textContent !== '' || frag.querySelector?.('img'))) nel.appendChild(frag);
    ensureFillable(nel);
    ensureFillable(el);

    el.after(nel);
    this.markDirty(el);
    this.renumber();
    setCaret(nel, 0);
    this.touchDoc();
  }

  followerSpec(el) {
    if (el.classList.contains('li')) {
      return {
        type: 'li',
        ordered: el.dataset.ordered === '1',
        num: +(el.dataset.num || 1) + 1,
        delim: el.dataset.delim,
        indent: +(el.dataset.indent || 0),
        check: el.hasAttribute('data-check') ? false : null,
        text: '',
      };
    }
    if (el.tagName === 'BLOCKQUOTE') return { type: 'quote', level: +(el.dataset.level || 1), text: '' };
    return { type: 'p', text: '' };
  }

  // splitTail schneidet alles ab dem Cursor aus dem Block heraus.
  splitTail(el) {
    const s = getSelection();
    if (!s.rangeCount) return null;
    const r = s.getRangeAt(0);
    r.deleteContents();
    const tail = document.createRange();
    tail.setStart(r.startContainer, r.startOffset);
    tail.setEnd(el, el.childNodes.length);
    return tail.extractContents();
  }

  insertAfter(el, spec) {
    const nel = this.makeEl({ ...spec, trail: 1 });
    el.after(nel);
    setCaret(nel, 0);
    this.renumber();
    this.touchDoc();
    return nel;
  }

  onBackspace(e) {
    const s = getSelection();
    if (!s.rangeCount || !s.isCollapsed) return;
    const el = this.currentEl();
    if (!el || caretOffset(el) !== 0) return;

    e.preventDefault();
    this.snapshot();

    if (el.classList.contains('li')) {
      const ind = +(el.dataset.indent || 0);
      if (ind > 0) { this.setIndentAttrs(el, ind - 1); this.markDirty(el); this.renumber(); this.touchDoc(); return; }
      this.morph(el, { type: 'p' });
      return;
    }
    if (el.tagName === 'BLOCKQUOTE') {
      const lv = +(el.dataset.level || 1);
      if (lv > 1) { el.dataset.level = String(lv - 1); this.markDirty(el); this.touchDoc(); return; }
      this.morph(el, { type: 'p' });
      return;
    }
    if (/^H[1-6]$/.test(el.tagName)) { this.morph(el, { type: 'p' }); return; }

    const prev = el.previousElementSibling;
    if (!prev) { this.touchDoc(); return; }

    if (prev.classList.contains('rule')) {
      prev.remove();
      setCaret(el, 0);
      this.touchDoc();
      return;
    }
    if (prev.tagName === 'PRE' || prev.tagName === 'TABLE' || el.tagName === 'PRE' || el.tagName === 'TABLE') {
      if (isEmptyEl(el)) { el.remove(); setCaret(prev, prev.textContent.length); }
      else setCaret(prev, prev.textContent.length);
      this.touchDoc();
      return;
    }

    const at = prev.textContent.length;
    if (prev.childNodes.length === 1 && prev.firstChild.nodeName === 'BR') prev.replaceChildren();
    while (el.firstChild) {
      const n = el.firstChild;
      if (n.nodeName === 'BR' && !n.nextSibling) { el.removeChild(n); break; }
      prev.appendChild(n);
    }
    el.remove();
    ensureFillable(prev);
    this.markDirty(prev);
    this.renumber();
    setCaret(prev, at);
    this.touchDoc();
  }

  onDelete(e) {
    const s = getSelection();
    if (!s.rangeCount || !s.isCollapsed) return;
    const el = this.currentEl();
    if (!el || el.tagName === 'PRE' || el.tagName === 'TABLE') return;
    if (caretOffset(el) !== el.textContent.length) return;

    const next = el.nextElementSibling;
    if (!next) return;
    e.preventDefault();
    this.snapshot();

    if (next.classList.contains('rule')) { next.remove(); this.touchDoc(); return; }
    if (next.tagName === 'PRE' || next.tagName === 'TABLE') { setCaret(next, 0); return; }

    const at = el.textContent.length;
    if (el.childNodes.length === 1 && el.firstChild.nodeName === 'BR') el.replaceChildren();
    while (next.firstChild) el.appendChild(next.firstChild);
    next.remove();
    ensureFillable(el);
    this.markDirty(el);
    this.renumber();
    setCaret(el, at);
    this.touchDoc();
  }

  onTab(e) {
    const el = this.currentEl();
    if (!el) return;
    if (el.tagName === 'PRE') { e.preventDefault(); document.execCommand('insertText', false, '  '); return; }
    if (!el.classList.contains('li')) return;
    e.preventDefault();
    this.snapshot();
    const ind = +(el.dataset.indent || 0);
    const next = e.shiftKey ? Math.max(0, ind - 1) : Math.min(6, ind + 1);
    if (next === ind) return;
    this.setIndentAttrs(el, next);
    this.markDirty(el);
    this.renumber();
    this.touchDoc();
  }

  // -------------------------------------------------- Auszeichnen im Fluss

  // Die Auszeichnung passiert vor der Eingabe: sonst setzt der Browser die
  // Auswahl nach dem Ereignis wieder zurueck und der Cursor geht verloren.
  onBeforeInput(e) {
    if (this.suspend) return;
    if (e.inputType !== 'insertText' || !e.data) return;
    const el = this.currentEl();
    if (!el || el.tagName === 'PRE') return;

    if (e.data === ' ' && this.blockTrigger(el, e.data)) { e.preventDefault(); return; }
    if ('*`~)'.includes(e.data) && this.inlineTrigger(e.data)) e.preventDefault();
  }

  // blockTrigger prueft den Zeilenanfang samt des Zeichens, das gerade
  // getippt wird, und liefert true, wenn es den Block umgewandelt hat.
  blockTrigger(el, pending) {
    const pos = caretOffset(el);
    if (pos < 0) return false;
    const head = el.textContent.slice(0, pos).replace(/\u00A0/g, ' ') + pending;
    const isP = el.tagName === 'P';
    const cut = (m) => this.strip(el, m[0].length - pending.length);
    let m;

    if (isP && (m = /^(#{1,6}) $/.exec(head))) {
      cut(m);
      this.morph(el, { type: 'h', level: m[1].length });
      return true;
    }
    if (isP && (m = /^((?:> ?)+)$/.exec(head))) {
      cut(m);
      this.morph(el, { type: 'quote', level: (m[1].match(/>/g) || []).length });
      return true;
    }
    if (el.tagName === 'BLOCKQUOTE' && (m = /^> $/.exec(head))) {
      cut(m);
      el.dataset.level = String(Math.min(4, +(el.dataset.level || 1) + 1));
      ensureFillable(el);
      setCaret(el, 0);
      this.markDirty(el);
      this.touchDoc();
      return true;
    }
    if (isP && (m = /^(?:[-*+] )?\[([ xX]?)\] $/.exec(head))) {
      cut(m);
      this.morph(el, { type: 'li', ordered: false, delim: '-', indent: 0, check: !!m[1].trim() });
      return true;
    }
    if (isP && (m = /^([-*+]) $/.exec(head))) {
      cut(m);
      this.morph(el, { type: 'li', ordered: false, delim: m[1], indent: 0, check: null });
      return true;
    }
    if (isP && (m = /^(\d{1,9})([.)]) $/.exec(head))) {
      cut(m);
      this.morph(el, { type: 'li', ordered: true, num: +m[1], delim: m[2], indent: 0, check: null });
      return true;
    }
    if (el.classList.contains('li') && !el.hasAttribute('data-check') && (m = /^\[([ xX]?)\] $/.exec(head))) {
      cut(m);
      el.dataset.check = m[1].trim() ? '1' : '0';
      ensureFillable(el);
      setCaret(el, 0);
      this.markDirty(el);
      this.touchDoc();
      return true;
    }
    return false;
  }

  strip(el, n) {
    const a = pointAt(el, 0);
    const b = pointAt(el, n);
    const r = document.createRange();
    r.setStart(a.node, a.offset);
    r.setEnd(b.node, b.offset);
    r.deleteContents();
  }

  inlineTrigger(pending) {
    const s = getSelection();
    const node = s.focusNode;
    if (!node || node.nodeType !== TEXT) return false;
    const to = s.focusOffset;
    const before = node.data.slice(0, to).replace(/\u00A0/g, ' ') + pending;

    // Die Inhalte duerfen das eigene Auszeichnungszeichen nicht enthalten,
    // sonst schnappt *kursiv* schon in der Mitte von **fett** zu.
    const rules = [
      [/\*\*([^*\s](?:[^*]*[^*\s])?)\*\*$/, 'strong', 1, 0],
      [/~~([^~\s](?:[^~]*[^~\s])?)~~$/, 's', 1, 0],
      [/`([^`]+)`$/, 'code', 1, 0],
      [/(^|[^*\w])\*([^*\s](?:[^*]*[^*\s])?)\*$/, 'em', 2, 1],
      [/\[([^\]]+)\]\(([^)\s]+)\)$/, 'a', 1, 0],
    ];

    for (const [re, tag, group, skip] of rules) {
      const m = re.exec(before);
      if (!m) continue;
      const from = m.index + (skip ? m[1].length : 0);
      if (from >= to) continue;
      this.snapshot();
      this.wrapInline(node, from, to, tag, m[group], tag === 'a' ? m[2] : null);
      this.touchDoc();
      return true;
    }
    return false;
  }

  wrapInline(node, from, to, tag, inner, href) {
    const r = document.createRange();
    r.setStart(node, from);
    r.setEnd(node, to);
    r.deleteContents();

    const e = document.createElement(tag);
    e.textContent = inner;
    if (href) { e.setAttribute('href', href); e.dataset.href = href; }
    r.insertNode(e);

    const tail = document.createTextNode('\u200B');
    e.after(tail);
    const rr = document.createRange();
    rr.setStart(tail, 1);
    rr.collapse(true);
    const s = getSelection();
    s.removeAllRanges();
    s.addRange(rr);
  }

  linkSelection() {
    const s = getSelection();
    if (!s.rangeCount || s.isCollapsed) return;
    const url = prompt('Adresse');
    if (!url) return;
    this.snapshot();
    document.execCommand('createLink', false, url);
    this.touchDoc();
  }

  morph(el, spec) {
    const b = this.blockOf(el);
    const pos = Math.max(0, caretOffset(el));
    const nb = { ...spec, text: '', trail: b ? b.trail : 1, lead: b ? b.lead : 0 };
    const nel = this.makeEl(nb);
    nel.replaceChildren();
    while (el.firstChild) nel.appendChild(el.firstChild);
    ensureFillable(nel);
    el.replaceWith(nel);
    this.renumber();
    setCaret(nel, pos);
    this.touchDoc();
    return nel;
  }

  // ----------------------------------------------------------- Einfuegen

  onPaste(e) {
    const text = e.clipboardData?.getData('text/plain');
    if (text == null) return;
    e.preventDefault();
    this.snapshot();

    if (!text.includes('\n')) {
      document.execCommand('insertText', false, text);
      return;
    }

    const el = this.currentEl();
    if (!el) return;
    const { blocks } = md.parse(text.endsWith('\n') ? text : text + '\n');
    if (!blocks.length) return;

    let anchor = el;
    let last = el;
    for (const b of blocks) {
      b.dirty = true;
      const ne = this.makeEl(b);
      anchor.after(ne);
      anchor = ne;
      last = ne;
    }
    if (isEmptyEl(el)) el.remove();
    this.renumber();
    setCaret(last, last.textContent.length);
    this.touchDoc();
  }

  // ------------------------------------------------------------ Verlauf

  caretMark() {
    const el = this.currentEl();
    if (!el) return { i: 0, off: 0 };
    return { i: [...this.root.children].indexOf(el), off: Math.max(0, caretOffset(el)) };
  }

  snapshot() {
    clearTimeout(this.snapTimer);
    if (this.suspend) return;
    const { text } = this.serialize();
    const top = this.history[this.history.length - 1];
    if (top && top.text === text) return;
    this.history.push({ text, caret: this.caretMark() });
    if (this.history.length > 200) this.history.shift();
    this.future.length = 0;
  }

  undo() {
    const cur = this.serialize().text;
    let prev = null;
    while (this.history.length) {
      const s = this.history.pop();
      if (s.text !== cur) { prev = s; break; }
    }
    if (!prev) return;
    this.future.push({ text: cur, caret: this.caretMark() });
    this.restore(prev);
  }

  redo() {
    const next = this.future.pop();
    if (!next) return;
    this.history.push({ text: this.serialize().text, caret: this.caretMark() });
    this.restore(next);
  }

  restore(state) {
    this.render(state.text);
    const el = this.root.children[Math.min(state.caret.i, this.root.children.length - 1)];
    if (el) setCaret(el, state.caret.off);
    this.onChange();
  }

  focus() {
    const first = this.root.firstElementChild;
    if (first) setCaret(first, 0);
    this.root.focus({ preventScroll: true });
  }
}

// tagFor beschriftet eine Randmarke: wer, wann.
function tagFor(r) {
  const d = new Date(r.time);
  if (isNaN(d)) return `${r.user}\n${r.time}`;
  const p2 = (n) => String(n).padStart(2, '0');
  const when = `${p2(d.getDate())}.${p2(d.getMonth() + 1)}. ${p2(d.getHours())}:${p2(d.getMinutes())}`;
  return `${r.user}\n${when}`;
}
