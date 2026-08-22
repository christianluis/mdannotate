// Markdown in Bloecke zerlegen, als DOM darstellen und wieder zurueckschreiben.
//
// Der Trick fuer verlustfreies Arbeiten: jeder Block merkt sich seine
// Quellzeilen. Beim Speichern werden nur die Bloecke neu erzeugt, die
// tatsaechlich angefasst wurden — alles andere geht Zeichen fuer Zeichen so
// zurueck in die Datei, wie es hereinkam.

const RE_H     = /^(#{1,6})[ \t]+(.*)$/;
const RE_FENCE = /^([ \t]{0,3})(`{3,}|~{3,})[ \t]*([\w+#.-]*)[ \t]*$/;
const RE_HR    = /^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$/;
const RE_UL    = /^([ \t]*)([-*+])[ \t]+(.*)$/;
const RE_OL    = /^([ \t]*)(\d{1,9})([.)])[ \t]+(.*)$/;
const RE_QUOTE = /^[ \t]{0,3}((?:>[ \t]?)+)(.*)$/;
const RE_TASK  = /^\[([ xX])\][ \t]+(.*)$/;
const RE_TROW  = /^[ \t]{0,3}\|(.*)\|[ \t]*$/;
const RE_TSEP  = /^[ \t]{0,3}\|?[ \t]*:?-{2,}:?[ \t]*(?:\|[ \t]*:?-{2,}:?[ \t]*)*\|?[ \t]*$/;
const RE_HTML  = /^[ \t]{0,3}<[a-zA-Z!/]/;

const blank = (l) => l === undefined || l.trim() === '';

// startsBlock sagt, ob eine Zeile einen neuen Block eroeffnet und deshalb
// einen laufenden Absatz beendet.
function startsBlock(l) {
  return RE_H.test(l) || RE_FENCE.test(l) || RE_HR.test(l) || RE_QUOTE.test(l) ||
         RE_UL.test(l) || RE_OL.test(l) || RE_HTML.test(l);
}

// ---------------------------------------------------------------- Parsen

export function parse(text) {
  const finalNL = text === '' || text.endsWith('\n');
  const lines = text.split('\n');
  if (finalNL && lines.length) lines.pop();

  const blocks = [];
  let i = 0;
  let lead = 0;

  const step = detectStep(lines);

  const push = (b, start, end) => {
    let t = end;
    while (t < lines.length && blank(lines[t])) t++;
    b.src = lines.slice(start - lead, end);
    b.lead = lead;
    b.trail = t - end;
    b.dirty = false;
    blocks.push(b);
    lead = 0;
    i = t;
  };

  // Front Matter steht nur ganz oben.
  if (lines[0] === '---') {
    const close = lines.indexOf('---', 1);
    if (close > 0) {
      push({ type: 'front', lines: lines.slice(1, close) }, 0, close + 1);
    }
  }

  while (i < lines.length) {
    const start = i;
    const line = lines[i];

    if (blank(line)) { lead++; i++; continue; }

    let m;

    if ((m = RE_FENCE.exec(line))) {
      const [, ind, fence, lang] = m;
      let j = i + 1;
      while (j < lines.length && !new RegExp('^[ \\t]{0,3}' + fence[0] + '{' + fence.length + ',}[ \\t]*$').test(lines[j])) j++;
      const inner = lines.slice(i + 1, Math.min(j, lines.length));
      push({ type: 'code', lang, indent: ind, lines: inner }, start, Math.min(j + 1, lines.length));
      continue;
    }

    if ((m = RE_H.exec(line))) {
      push({ type: 'h', level: m[1].length, text: m[2].replace(/[ \t]+#+[ \t]*$/, '') }, start, i + 1);
      continue;
    }

    if (RE_HR.test(line)) {
      push({ type: 'hr' }, start, i + 1);
      continue;
    }

    if ((m = RE_QUOTE.exec(line))) {
      const level = (m[1].match(/>/g) || []).length;
      const body = [m[2]];
      let j = i + 1;
      while (j < lines.length) {
        const q = RE_QUOTE.exec(lines[j]);
        if (!q || (q[1].match(/>/g) || []).length !== level) break;
        body.push(q[2]);
        j++;
      }
      push({ type: 'quote', level, text: body.join('\n').replace(/\n+$/, '') }, start, j);
      continue;
    }

    const ul = RE_UL.exec(line);
    const ol = ul ? null : RE_OL.exec(line);
    if (ul || ol) {
      const pad = (ul || ol)[1].replace(/\t/g, '  ').length;
      const marker = ul ? ul[2] : ol[2] + ol[3];
      let body = ul ? ul[3] : ol[4];
      let check = null;
      const task = RE_TASK.exec(body);
      if (task) { check = task[1] !== ' '; body = task[2]; }

      const cont = [body];
      let j = i + 1;
      while (j < lines.length && !blank(lines[j]) && !startsBlock(lines[j]) && !RE_TROW.test(lines[j])) {
        cont.push(lines[j].trim());
        j++;
      }
      push({
        type: 'li',
        ordered: !!ol,
        num: ol ? parseInt(ol[2], 10) : 0,
        delim: ol ? ol[3] : marker,
        indent: pad === 0 ? 0 : Math.max(1, Math.round(pad / step)),
        step,
        check,
        text: cont.join('\n'),
      }, start, j);
      continue;
    }

    if (RE_TROW.test(line) && RE_TSEP.test(lines[i + 1] || '')) {
      const align = splitRow(lines[i + 1]).map(c =>
        /^:-+:$/.test(c) ? 'center' : /^:-+$/.test(c) ? 'left' : /^-+:$/.test(c) ? 'right' : '');
      const rows = [splitRow(line)];
      let j = i + 2;
      while (j < lines.length && RE_TROW.test(lines[j])) { rows.push(splitRow(lines[j])); j++; }
      push({ type: 'table', align, rows }, start, j);
      continue;
    }

    if (RE_HTML.test(line)) {
      let j = i;
      while (j < lines.length && !blank(lines[j])) j++;
      push({ type: 'html', lines: lines.slice(i, j) }, start, j);
      continue;
    }

    let j = i;
    const body = [];
    while (j < lines.length && !blank(lines[j]) && !(j > i && startsBlock(lines[j]))) {
      body.push(lines[j]);
      j++;
    }
    push({ type: 'p', text: body.join('\n') }, start, j);
  }

  // Leerzeilen am Dateiende gehoeren an den letzten Block.
  if (lead > 0 && blocks.length) blocks[blocks.length - 1].trail += lead;
  else if (lead > 0) blocks.push({ type: 'p', text: '', src: lines.slice(0, lead), lead: 0, trail: 0, dirty: false });

  return { blocks, finalNL, step };
}

// detectStep raet die Einrueckbreite verschachtelter Listen (2 oder 4).
function detectStep(lines) {
  let four = 0, two = 0;
  for (const l of lines) {
    const m = RE_UL.exec(l) || RE_OL.exec(l);
    if (!m) continue;
    const w = m[1].replace(/\t/g, '  ').length;
    if (w === 0) continue;
    if (w % 4 === 0) four++; else if (w % 2 === 0) two++;
  }
  return four > two ? 4 : 2;
}

function splitRow(line) {
  return line.trim().replace(/^\|/, '').replace(/\|[ \t]*$/, '')
    .split(/(?<!\\)\|/).map(c => c.trim().replace(/\\\|/g, '|'));
}

// ----------------------------------------------------- Bloecke -> Zeilen

export function blockToLines(b) {
  const out = [];
  for (let i = 0; i < (b.lead || 0); i++) out.push('');
  out.push(...body(b));
  return out;
}

function body(b) {
  switch (b.type) {
    case 'h':
      return ['#'.repeat(b.level) + ' ' + flat(b.text)];

    case 'p':
      return b.text === '' ? [''] : b.text.split('\n');

    case 'quote': {
      const p = '> '.repeat(Math.max(1, b.level));
      return b.text.split('\n').map(l => (l === '' ? p.trimEnd() : p + l));
    }

    case 'li': {
      const pad = ' '.repeat((b.indent || 0) * (b.step || 2));
      const marker = b.ordered ? b.num + (b.delim || '.') : (b.delim && '-*+'.includes(b.delim) ? b.delim : '-');
      const box = b.check === null || b.check === undefined ? '' : (b.check ? '[x] ' : '[ ] ');
      const parts = (b.text === '' ? [''] : b.text.split('\n'));
      const hang = pad + ' '.repeat(marker.length + 1);
      return [pad + marker + ' ' + box + parts[0], ...parts.slice(1).map(l => hang + l)];
    }

    case 'code': {
      const longest = b.lines.reduce((n, l) => {
        const m = /^[ \t]{0,3}(`{3,})/.exec(l);
        return m ? Math.max(n, m[1].length) : n;
      }, 2);
      const fence = '`'.repeat(Math.max(3, longest + 1));
      return [fence + (b.lang || ''), ...b.lines, fence];
    }

    case 'hr':    return ['---'];
    case 'front': return ['---', ...b.lines, '---'];
    case 'html':  return b.lines.slice();

    case 'table': {
      const cols = Math.max(...b.rows.map(r => r.length), b.align.length);
      const cell = (r, c) => (r[c] ?? '').replace(/\|/g, '\\|');
      const width = [];
      for (let c = 0; c < cols; c++) {
        width[c] = Math.max(3, ...b.rows.map(r => cell(r, c).length));
      }
      const line = (r) => '| ' + Array.from({ length: cols }, (_, c) => cell(r, c).padEnd(width[c])).join(' | ') + ' |';
      const sep = '| ' + Array.from({ length: cols }, (_, c) => {
        const a = b.align[c] || '';
        const marks = a === 'center' ? 2 : a ? 1 : 0;
        const bar = '-'.repeat(Math.max(1, width[c] - marks));
        return a === 'center' ? ':' + bar + ':' : a === 'right' ? bar + ':' : a === 'left' ? ':' + bar : bar;
      }).join(' | ') + ' |';
      return [line(b.rows[0] || []), sep, ...b.rows.slice(1).map(line)];
    }

    default:
      return [flat(b.text || '')];
  }
}

const flat = (s) => (s || '').replace(/[ \t]*\n[ \t]*/g, ' ');

// ------------------------------------------------------- Inline -> HTML

const esc = (s) => s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

export function inlineToHTML(md, ctx = {}) {
  let out = '';
  let i = 0;
  const prev = () => (i > 0 ? md[i - 1] : '');

  while (i < md.length) {
    const rest = md.slice(i);
    let m;

    if ((m = /^(`+)([\s\S]*?)\1(?!`)/.exec(rest))) {
      out += '<code>' + esc(m[2]) + '</code>'; i += m[0].length; continue;
    }
    if ((m = /^!\[([^\]]*)\]\(([^)\s]*)(?:[ \t]+"[^"]*")?\)/.exec(rest))) {
      out += `<img src="${esc(assetURL(m[2], ctx))}" data-src="${esc(m[2])}" alt="${esc(m[1])}">`;
      i += m[0].length; continue;
    }
    if ((m = /^\[([^\]]*)\]\(([^)\s]*)(?:[ \t]+"[^"]*")?\)/.exec(rest))) {
      out += `<a href="${esc(m[2])}" data-href="${esc(m[2])}">${inlineToHTML(m[1], ctx)}</a>`;
      i += m[0].length; continue;
    }
    if ((m = /^\*\*(?!\s)([\s\S]+?)(?<!\s)\*\*/.exec(rest))) {
      out += '<strong>' + inlineToHTML(m[1], ctx) + '</strong>'; i += m[0].length; continue;
    }
    if ((m = /^__(?!\s)([\s\S]+?)(?<!\s)__/.exec(rest)) && !/\w/.test(prev())) {
      out += '<strong>' + inlineToHTML(m[1], ctx) + '</strong>'; i += m[0].length; continue;
    }
    if ((m = /^~~([\s\S]+?)~~/.exec(rest))) {
      out += '<s>' + inlineToHTML(m[1], ctx) + '</s>'; i += m[0].length; continue;
    }
    if ((m = /^\*(?!\s)([^*\n]+?)(?<!\s)\*/.exec(rest))) {
      out += '<em>' + inlineToHTML(m[1], ctx) + '</em>'; i += m[0].length; continue;
    }
    // Unterstriche nur an Wortgrenzen, damit snake_case heil bleibt.
    if ((m = /^_(?!\s)([^_\n]+?)(?<!\s)_(?!\w)/.exec(rest)) && !/\w/.test(prev())) {
      out += '<em>' + inlineToHTML(m[1], ctx) + '</em>'; i += m[0].length; continue;
    }
    if (rest.startsWith('  \n') || rest.startsWith('\\\n')) {
      out += '<br>'; i += rest[0] === '\\' ? 2 : 3; continue;
    }
    if (rest[0] === '\n') { out += ' '; i += 1; continue; }
    if (rest[0] === '\\' && rest.length > 1 && /[\\`*_{}\[\]()#+\-.!>~|]/.test(rest[1])) {
      out += esc(rest[1]); i += 2; continue;
    }

    out += esc(md[i]); i++;
  }
  return out;
}

function assetURL(src, ctx) {
  if (/^(https?:|data:|mailto:|#|\/\/)/i.test(src)) return src;
  const dir = ctx.dir || '';
  const joined = dir ? dir + '/' + src : src;
  return `/asset?path=${encodeURIComponent(joined)}&t=${encodeURIComponent(ctx.token || '')}`;
}

// ------------------------------------------------------- DOM -> Inline

export function inlineFromDOM(node) {
  let out = '';
  for (const c of node.childNodes) {
    if (c.nodeType === Node.TEXT_NODE) {
      // Browser setzen beim Tippen gern geschuetzte Leerzeichen — die haben
      // in einer Markdown-Datei nichts verloren.
      out += c.data.replace(/\u200B/g, '').replace(/\u00A0/g, ' ').replace(/[\r\n]+/g, ' ');
      continue;
    }
    if (c.nodeType !== Node.ELEMENT_NODE) continue;

    switch (c.tagName) {
      case 'BR':     out += '  \n'; break;
      case 'STRONG':
      case 'B':      out += wrapIf('**', inlineFromDOM(c)); break;
      case 'EM':
      case 'I':      out += wrapIf('*', inlineFromDOM(c)); break;
      case 'S':
      case 'DEL':
      case 'STRIKE': out += wrapIf('~~', inlineFromDOM(c)); break;
      case 'CODE':   out += wrapIf('`', c.textContent.replace(/\u200B/g, '').replace(/\u00A0/g, ' ')); break;
      case 'IMG':    out += `![${c.alt || ''}](${c.dataset.src || c.getAttribute('src') || ''})`; break;
      case 'A':      out += `[${inlineFromDOM(c)}](${c.dataset.href || c.getAttribute('href') || ''})`; break;
      default:       out += inlineFromDOM(c);
    }
  }
  return out;
}

// wrapIf laesst leere Auszeichnungen weg, statt `****` zu schreiben.
function wrapIf(mark, inner) {
  return inner.trim() === '' ? inner : mark + inner + mark;
}

// blockText liest den Inhalt eines Blockelements als Markdown.
export function blockText(el) {
  return inlineFromDOM(el).replace(/(?:[ \t]*\n)+$/, '').replace(/[ \t]+$/, '');
}
