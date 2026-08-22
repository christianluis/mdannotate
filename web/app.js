// Verdrahtung: Dateibaum, Laden, Sichern, Live-Aktualisierung, Darstellung.

import { Editor } from './editor.js';

const token = new URLSearchParams(location.search).get('t')
  || sessionStorage.getItem('mda.token') || '';
if (token) sessionStorage.setItem('mda.token', token);

const $ = (id) => document.getElementById(id);
const treeEl = $('tree');
const docEl = $('doc');
const pageEl = $('page');
const emptyEl = $('empty');
const statusEl = $('status');
const marksEl = $('markCount');
const crumbsEl = $('crumbs');
const filterEl = $('filter');
const bannerEl = $('banner');
const bannerText = $('bannerText');
const bannerAction = $('bannerAction');
const annBtn = $('annBtn');
const annLabel = $('annLabel');
const shelfEl = $('shelf');
const versionsEl = $('versions');
const shelfFoot = $('shelfFoot');
const versBtn = $('versBtn');
const diffSel = $('diffSel');

let cfg = {};
let current = null;
let mod = 0;
let lastSaved = null;
let saving = false;
let pending = false;
let conflicted = false;
let saveTimer = 0;
let marking = true;
let files = [];
const collapsed = new Set();

// Fassungen der geoeffneten Datei. viewing ist die gerade angesehene
// fruehere Fassung — solange sie steht, ist der Editor nur zum Lesen da.
let versions = [];
let viewing = null;
// compared ist die Fassung, mit der die angesehene gerade verglichen wird.
let compared = null;
let shelfOpen = false;
let vinfo = { git: false, changes: '' };
// Womit eine angesehene Fassung verglichen wird: mit der vorigen, mit dem
// Arbeitsstand oder mit nichts.
let diffMode = 'prev';

const ed = new Editor(docEl, { onChange: scheduleSave });

// Fuer die Konsole: mda.ed.serialize() zeigt, was gleich in der Datei steht.
window.mda = { ed, save: () => save(), open: (p) => openFile(p) };

// ------------------------------------------------------------------ API

async function api(path, opts = {}) {
  const r = await fetch(path, {
    ...opts,
    headers: { 'X-Mda-Token': token, ...(opts.headers || {}) },
  });
  if (!r.ok && r.status !== 409) throw new Error((await r.text()) || r.statusText);
  return r;
}

// ------------------------------------------------------------- Statuszeile

function setStatus(state, text) {
  statusEl.dataset.state = state || '';
  statusEl.textContent = text;
}

function setMarks(n) {
  marksEl.hidden = !n;
  marksEl.textContent = n === 1 ? '1 Marke' : `${n} Marken`;
}

// Im Vergleich zaehlt die Kopfzeile die Stellen, an denen sich etwas tut.
function setChanges(n, gegen) {
  marksEl.hidden = false;
  marksEl.textContent = n === 0 ? 'keine Unterschiede'
    : n === 1 ? '1 Unterschied' : `${n} Unterschiede`;
  marksEl.title = gegen ? 'gegenüber ' + gegen : '';
}

function showBanner(text, actionLabel, action) {
  bannerText.textContent = text;
  bannerAction.textContent = actionLabel;
  bannerAction.onclick = action;
  bannerEl.hidden = false;
}

function hideBanner() {
  bannerEl.hidden = true;
  diffSel.hidden = true;
  for (const b of bannerEl.querySelectorAll('.extra')) b.remove();
  conflicted = false;
}

// ------------------------------------------------------------- Dateibaum

let treeRoot = null;

async function loadTree() {
  treeRoot = await (await api('/api/tree')).json();
  files = [];
  collect(treeRoot);
  drawTree(treeRoot);
}

function collect(n) {
  for (const c of n.children || []) {
    if (c.dir) collect(c);
    else files.push(c);
  }
}

function drawTree(root) {
  const q = filterEl.value.trim().toLowerCase();
  treeEl.replaceChildren();

  if (q) {
    const hits = files.filter((f) => f.path.toLowerCase().includes(q)).slice(0, 200);
    if (!hits.length) {
      const p = document.createElement('p');
      p.className = 'row dir';
      p.textContent = 'nichts gefunden';
      treeEl.appendChild(p);
      return;
    }
    for (const f of hits) treeEl.appendChild(fileRow(f, true));
    return;
  }

  for (const c of root.children || []) treeEl.appendChild(node(c));
  if (!root.children?.length) {
    const p = document.createElement('p');
    p.className = 'row dir';
    p.textContent = 'keine Markdown-Dateien';
    treeEl.appendChild(p);
  }
}

function node(n) {
  if (!n.dir) return fileRow(n, false);

  const wrap = document.createElement('div');
  wrap.className = 'tree-group';
  if (collapsed.has(n.path)) wrap.classList.add('closed');

  const row = document.createElement('button');
  row.type = 'button';
  row.className = 'row dir';
  row.innerHTML = '<span class="caret">▾</span>';
  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = n.name;
  row.appendChild(name);
  row.addEventListener('click', () => {
    wrap.classList.toggle('closed');
    if (wrap.classList.contains('closed')) collapsed.add(n.path);
    else collapsed.delete(n.path);
  });

  const kids = document.createElement('div');
  kids.className = 'tree-kids';
  for (const c of n.children || []) kids.appendChild(node(c));

  wrap.append(row, kids);
  return wrap;
}

function fileRow(f, showPath) {
  const row = document.createElement('button');
  row.type = 'button';
  row.className = 'row file';
  row.dataset.path = f.path;
  if (f.path === current) row.classList.add('current');

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = showPath ? f.path : f.name;
  row.appendChild(name);

  if (f.marks) {
    const c = document.createElement('span');
    c.className = 'count';
    c.textContent = f.marks;
    c.title = f.marks === 1 ? '1 markierte Passage' : `${f.marks} markierte Passagen`;
    row.appendChild(c);
  }
  row.addEventListener('click', () => openFile(f.path));
  return row;
}

function markCurrentRow() {
  for (const el of treeEl.querySelectorAll('.row.file')) {
    el.classList.toggle('current', el.dataset.path === current);
  }
}

function updateRowCount(path, marks) {
  const row = treeEl.querySelector(`.row.file[data-path="${CSS.escape(path)}"]`);
  const f = files.find((x) => x.path === path);
  if (f) f.marks = marks;
  if (!row) return;
  let c = row.querySelector('.count');
  if (!marks) { c?.remove(); return; }
  if (!c) { c = document.createElement('span'); c.className = 'count'; row.appendChild(c); }
  c.textContent = marks;
}

// ---------------------------------------------------------------- Datei

async function openFile(path) {
  const wechsel = path !== current;
  // Beim Nachladen derselben Datei bleibt die Leseposition stehen.
  const scroll = wechsel ? 0 : pageEl.scrollTop;
  if (current && wechsel) await flush();
  // Wer eine Datei oeffnet, will sie bearbeiten, nicht eine alte Fassung lesen.
  viewing = null;
  compared = null;
  ed.setReadOnly(false);
  pageEl.classList.remove('reading');
  const r = await api(`/api/file?path=${encodeURIComponent(path)}`);
  const data = await r.json();

  current = path;
  mod = data.mod;
  lastSaved = data.text;
  hideBanner();

  ed.load(data.text, { dir: path.split('/').slice(0, -1).join('/'), token });
  ed.applyRegions(data.regions);

  docEl.hidden = false;
  emptyEl.hidden = true;
  drawCrumbs(path);
  setMarks(data.marks);
  setStatus('', 'geöffnet');
  markCurrentRow();
  localStorage.setItem(lastKey(), path);
  history.replaceState(null, '', '#' + encodeURIComponent(path));

  // Eine neue Datei fängt oben an, nicht dort, wo die letzte aufhörte.
  pageEl.scrollTop = scroll;
  if (wechsel) {
    ed.focus();
    pageEl.scrollTop = 0;
  }
  if (shelfOpen) loadVersions();
}

function drawCrumbs(path) {
  crumbsEl.replaceChildren();
  const parts = path.split('/');
  parts.forEach((p, i) => {
    if (i) {
      const sep = document.createElement('span');
      sep.className = 'sep';
      sep.textContent = '/';
      crumbsEl.appendChild(sep);
    }
    const s = document.createElement('span');
    if (i === parts.length - 1) s.className = 'leaf';
    s.textContent = p;
    crumbsEl.appendChild(s);
  });
}

// --------------------------------------------------------------- Sichern

function scheduleSave() {
  if (!current || viewing) return;
  setStatus('dirty', 'ungesichert');
  clearTimeout(saveTimer);
  saveTimer = setTimeout(save, 900);
}

async function save(force = false) {
  if (!current || viewing) return;
  if (conflicted && !force) return;
  if (saving) { pending = true; return; }

  const { text } = ed.serialize();
  if (text === lastSaved && !force) {
    setStatus('saved', 'gesichert');
    return;
  }

  saving = true;
  setStatus('', 'sichern …');
  try {
    const r = await api(`/api/file?path=${encodeURIComponent(current)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text, mod: force ? 0 : mod, annotate: marking }),
    });
    const data = await r.json();

    if (r.status === 409) {
      conflicted = true;
      setStatus('dirty', 'Konflikt');
      showBanner('Diese Datei wurde außerhalb von mda geändert.', 'fremde Fassung laden', () => {
        hideBanner();
        openFile(current);
      });
      addKeepButton();
      return;
    }

    mod = data.mod;
    lastSaved = data.text;
    ed.applyRegions(data.regions);
    setMarks(data.marks);
    updateRowCount(current, data.marks);
    setStatus('saved', 'gesichert ' + new Date().toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' }));
    if (shelfOpen) loadVersions();
  } catch (err) {
    setStatus('dirty', 'nicht gesichert: ' + err.message);
  } finally {
    saving = false;
    if (pending) { pending = false; scheduleSave(); }
  }
}

// addKeepButton bietet im Konfliktfall an, die eigene Fassung durchzusetzen.
function addKeepButton() {
  if (bannerEl.querySelector('.keep')) return;
  const b = document.createElement('button');
  b.className = 'link-btn extra keep';
  b.textContent = 'eigene Fassung sichern';
  b.addEventListener('click', () => {
    b.remove();
    hideBanner();
    save(true);
  });
  bannerEl.appendChild(b);
}

async function flush() {
  clearTimeout(saveTimer);
  if (!current || conflicted || viewing) return;
  const { text } = ed.serialize();
  if (text !== lastSaved) await save();
}

// ------------------------------------------------------- Fremdaenderungen

function listen() {
  if (!token) return;
  const es = new EventSource(`/api/events?t=${encodeURIComponent(token)}`);

  es.addEventListener('error', () => {
    if (es.readyState !== EventSource.CLOSED) return;
    showBanner('Die Verbindung zu mda ist abgerissen. Läuft das Programm noch?', 'Seite neu laden', () => location.reload());
  });

  es.addEventListener('message', (e) => {
    let m;
    try { m = JSON.parse(e.data); } catch { return; }

    if (m.type === 'tree') { loadTree().then(markCurrentRow); return; }

    if (m.type === 'file') {
      if (m.path !== current) { loadTree().then(markCurrentRow); return; }
      // Wer gerade eine alte Fassung liest, wird nicht unterbrochen; die
      // neue Fassung taucht in der Liste auf.
      if (viewing) { if (shelfOpen) loadVersions(); return; }
      const { text } = ed.serialize();
      if (text === lastSaved && !saving) {
        openFile(current).then(() => setStatus('', 'von außen aktualisiert'));
      } else {
        conflicted = true;
        showBanner('Diese Datei wurde gerade außerhalb von mda geändert.', 'fremde Fassung laden', () => {
          hideBanner();
          openFile(current);
        });
        addKeepButton();
      }
    }
  });
}

// ------------------------------------------------------------- Fassungen

// Das Regal zeigt, was von einer Datei bekannt ist: der Arbeitsstand, die
// Faenge dieser Sitzung aus ~/.mda/changes und die Commits aus Git. Ein Klick
// legt eine davon in den Editor — zum Lesen, nicht zum Schreiben.

function toggleShelf(on) {
  shelfOpen = on === undefined ? !shelfOpen : on;
  shelfEl.hidden = !shelfOpen;
  versBtn.setAttribute('aria-pressed', String(shelfOpen));
  localStorage.setItem('mda.shelf', shelfOpen ? 'on' : 'off');
  if (shelfOpen) loadVersions();
}

async function loadVersions() {
  if (!current) { versions = []; drawVersions(); return; }
  try {
    const d = await (await api(`/api/versions?path=${encodeURIComponent(current)}`)).json();
    versions = d.versions || [];
    vinfo = { git: d.git, changes: d.changes || '' };
  } catch {
    versions = [];
  }
  drawVersions();
}

function drawVersions() {
  versionsEl.replaceChildren();
  if (!current) {
    versionsEl.appendChild(note('Erst eine Datei öffnen.'));
  } else if (!versions.length) {
    versionsEl.appendChild(note('Von dieser Datei ist noch keine Fassung bekannt.'));
  } else {
    for (const v of versions) versionsEl.appendChild(versionRow(v));
  }
  drawShelfFoot();
  markVersionRow();
}

function note(text) {
  const p = document.createElement('p');
  p.className = 'shelf-note';
  p.textContent = text;
  return p;
}

function versionRow(v) {
  const row = document.createElement('button');
  row.type = 'button';
  row.className = 'vrow';
  row.dataset.id = v.id;
  row.dataset.kind = v.kind;

  const dot = document.createElement('span');
  dot.className = 'vdot';

  const label = document.createElement('span');
  label.className = 'vlabel';
  label.textContent = v.label;

  const meta = document.createElement('span');
  meta.className = 'vmeta';
  meta.textContent = [stamp(v.time, v.kind !== 'git'), v.note].filter(Boolean).join('  ');

  row.append(dot, label, meta);
  row.title = versionTitle(v);
  row.addEventListener('click', () => showVersion(v));
  return row;
}

function drawShelfFoot() {
  shelfFoot.replaceChildren();
  const lines = [];
  if (vinfo.changes) lines.push('Fassungen dieser Sitzung: ' + tilde(vinfo.changes));
  if (!vinfo.git) lines.push('Kein Git-Archiv in Sicht.');
  for (const t of lines) {
    const p = document.createElement('p');
    p.textContent = t;
    shelfFoot.appendChild(p);
  }
}

// tilde kuerzt den Heimatordner weg; die Ablage liegt immer unter ~/.mda.
const tilde = (path) => path.replace(/^.*(\/\.mda\/)/, '~$1');

// stamp schreibt den Zeitpunkt: heute nur die Uhrzeit, sonst mit Datum.
function stamp(iso, seconds) {
  const d = new Date(iso);
  if (isNaN(d)) return iso || '';
  const opts = { hour: '2-digit', minute: '2-digit' };
  if (seconds) opts.second = '2-digit';
  const time = d.toLocaleTimeString('de-DE', opts);
  if (d.toDateString() === new Date().toDateString()) return time;
  return d.toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit' }) + ' ' + time;
}

function versionTitle(v) {
  const when = stamp(v.time, v.kind !== 'git');
  if (v.kind === 'git') return `Commit ${v.note || ''} · ${when} · ${v.label}`;
  if (v.kind === 'live') return 'Der Stand, der jetzt in der Datei steht';
  return `${v.label} · ${when}`;
}

// versionCaption steht im Balken ueber der angesehenen Fassung.
function versionCaption(v) {
  const when = stamp(v.time, v.kind !== 'git');
  if (v.kind === 'git') {
    const hash = (v.note || '').split(' ')[0];
    return `Nur Ansicht: Commit ${hash} von ${when} · „${v.label}“`;
  }
  return `Nur Ansicht: Fassung von ${when} · ${v.label}`;
}

function markVersionRow() {
  const id = viewing ? viewing.id : 'live';
  for (const el of versionsEl.querySelectorAll('.vrow')) {
    el.classList.toggle('current', el.dataset.id === id);
    const base = !!compared && el.dataset.id === compared.id;
    el.classList.toggle('base', base);
    if (base) el.title = 'Damit wird verglichen';
  }
}

// showVersion legt eine fruehere Fassung in den Editor. Was im Editor stand,
// ist vorher gesichert — im Lesemodus schreibt mda nichts mehr in die Datei.
async function showVersion(v) {
  if (!current || !v) return;
  if (v.kind === 'live') { await backToWork(); return; }
  await flush();

  const base = diffBase(v);
  const q = `path=${encodeURIComponent(current)}`;
  let data, diff = null;
  try {
    if (base) {
      diff = await (await api(`/api/diff?${q}&a=${encodeURIComponent(base.id)}&b=${encodeURIComponent(v.id)}`)).json();
    } else {
      data = await (await api(`/api/version?${q}&id=${encodeURIComponent(v.id)}`)).json();
    }
  } catch (err) {
    setStatus('dirty', 'Fassung nicht lesbar: ' + err.message);
    return;
  }

  viewing = v;
  ed.setReadOnly(true);
  ed.load((diff || data).text, { dir: current.split('/').slice(0, -1).join('/'), token });

  let caption = versionCaption(v);
  if (diff) {
    ed.applyDiff(diff.added, diff.removed);
    setChanges(diff.places, baseName(base));
  } else {
    ed.applyRegions(data.regions);
    setMarks(data.marks);
    marksEl.title = '';
    if (diffMode !== 'off') caption += ' · nichts zum Vergleichen';
  }
  compared = diff ? base : null;
  setStatus('reading', 'nur Ansicht');

  hideBanner();
  showBanner(caption, 'Ansicht beenden', backToWork);
  diffSel.hidden = false;
  addTakeButton();
  pageEl.classList.add('reading');

  markVersionRow();
  pageEl.scrollTop = 0;
  versionsEl.querySelector(`.vrow.current`)?.scrollIntoView({ block: 'nearest' });
}

// diffBase ist die Fassung, mit der verglichen wird — bei „zur vorigen“ die
// nächstältere aus der Liste, sonst der Arbeitsstand.
function diffBase(v) {
  if (diffMode === 'off') return null;
  if (diffMode === 'live') {
    const live = versions.find((x) => x.kind === 'live');
    return live && live.id !== v.id ? live : null;
  }
  const i = versions.findIndex((x) => x.id === v.id);
  return i < 0 ? null : versions[i + 1] || null;
}

function baseName(v) {
  if (!v) return '';
  if (v.kind === 'live') return 'dem Arbeitsstand';
  if (v.kind === 'git') return 'Commit ' + (v.note || '').split(' ')[0];
  return 'der Fassung von ' + stamp(v.time, true);
}

// Der Wahlschalter gilt sofort: die angesehene Fassung wird neu gezeichnet.
function applyDiffMode(mode) {
  diffMode = ['prev', 'live', 'off'].includes(mode) ? mode : 'prev';
  diffSel.value = diffMode;
  localStorage.setItem('mda.diff', diffMode);
}

diffSel.addEventListener('change', () => {
  applyDiffMode(diffSel.value);
  if (viewing) showVersion(viewing);
});

// backToWork holt zurueck, was auf der Platte steht.
async function backToWork() {
  if (!viewing) return;
  viewing = null;
  compared = null;
  ed.setReadOnly(false);
  pageEl.classList.remove('reading');
  hideBanner();
  await openFile(current);
  markVersionRow();
}

// addTakeButton uebernimmt die angesehene Fassung als neuen Text. Gesichert
// wird sie wie jede andere Änderung, also mit Marken, wenn der Schalter steht.
function addTakeButton() {
  const b = document.createElement('button');
  b.className = 'link-btn extra';
  b.textContent = 'diese Fassung übernehmen';
  b.addEventListener('click', async () => {
    const v = viewing;
    if (!v) return;
    // Im Vergleich stehen beide Fassungen im Editor. Übernommen wird die
    // angesehene, also wird sie noch einmal für sich geholt.
    let text;
    try {
      const r = await api(`/api/version?path=${encodeURIComponent(current)}&id=${encodeURIComponent(v.id)}`);
      text = (await r.json()).text;
    } catch (err) {
      setStatus('dirty', 'Fassung nicht lesbar: ' + err.message);
      return;
    }

    viewing = null;
    compared = null;
    ed.setReadOnly(false);
    pageEl.classList.remove('reading');
    ed.load(text, { dir: current.split('/').slice(0, -1).join('/'), token });
    ed.applyRegions([]);
    hideBanner();
    markVersionRow();
    setStatus('dirty', 'ungesichert');
    ed.focus();
    save();
  });
  bannerEl.appendChild(b);
}

// stepVersion blaettert in der Liste: nach unten in die Vergangenheit.
function stepVersion(delta) {
  if (!versions.length) return;
  const id = viewing ? viewing.id : 'live';
  let i = versions.findIndex((v) => v.id === id);
  if (i < 0) i = 0;
  const next = versions[Math.min(versions.length - 1, Math.max(0, i + delta))];
  if (next && next.id !== id) showVersion(next);
}

versBtn.addEventListener('click', () => toggleShelf());
$('shelfClose').addEventListener('click', () => toggleShelf(false));

// ---------------------------------------------------------- Marken an/aus

// Bei "aus" wandert der Text ohne Start- und Endmarke in die Datei; die
// Marken, die schon darin stehen, bleiben unangetastet.
function applyMarking(on) {
  marking = on;
  annBtn.setAttribute('aria-pressed', String(on));
  annLabel.textContent = on ? 'Marken an' : 'Marken aus';
  annBtn.title = on
    ? 'Jede Änderung wird mit Start- und Endmarke eingefasst. Klicken schaltet das ab.'
    : 'Änderungen gehen ohne Marken in die Datei. Klicken schaltet die Marken wieder an.';
  localStorage.setItem('mda.marking', on ? 'on' : 'off');
}

// Umgeschaltet wird fuer den naechsten Speichervorgang: was noch ungesichert
// im Editor steht, geht also schon nach der neuen Einstellung in die Datei.
annBtn.addEventListener('click', () => applyMarking(!marking));

// ------------------------------------------------------------ Darstellung

// Hell ist der Standard; dunkel nur, wenn jemand danach fragt.
function applyTheme(t) {
  document.documentElement.dataset.theme = t;
  localStorage.setItem('mda.theme', t);
  const btn = $('themeBtn');
  btn.textContent = t === 'dark' ? '☾' : '☀';
  btn.title = t === 'dark' ? 'Auf helle Darstellung wechseln' : 'Auf dunkle Darstellung wechseln';
}

$('themeBtn').addEventListener('click', () => {
  applyTheme(localStorage.getItem('mda.theme') === 'dark' ? 'light' : 'dark');
});

// ------------------------------------------------------------------ Start

const lastKey = () => 'mda.last:' + (cfg.root || '');

// Filtern arbeitet auf dem zuletzt geladenen Baum, ohne erneut zu fragen.
filterEl.addEventListener('input', () => {
  if (treeRoot) drawTree(treeRoot);
});

document.addEventListener('keydown', (e) => {
  const mod2 = e.metaKey || e.ctrlKey;
  const inFilter = document.activeElement === filterEl;
  if (mod2 && e.shiftKey && e.key.toLowerCase() === 'h') { e.preventDefault(); toggleShelf(); return; }
  if (mod2 && e.key.toLowerCase() === 's') { e.preventDefault(); clearTimeout(saveTimer); save(); }
  if (mod2 && e.key.toLowerCase() === 'p') { e.preventDefault(); filterEl.focus(); filterEl.select(); }

  // Solange eine fruehere Fassung im Editor liegt, blaettern die Pfeile
  // darin weiter; der Text selbst ruehrt sich ohnehin nicht.
  if (viewing && !mod2 && !inFilter && document.activeElement !== diffSel) {
    if (e.key === 'ArrowDown') { e.preventDefault(); stepVersion(1); return; }
    if (e.key === 'ArrowUp') { e.preventDefault(); stepVersion(-1); return; }
  }
  if (e.key === 'Escape' && viewing && !inFilter) { e.preventDefault(); backToWork(); return; }

  if (e.key === 'Escape' && inFilter) {
    filterEl.value = '';
    if (treeRoot) drawTree(treeRoot);
    ed.focus();
  }
});

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') flush();
});

window.addEventListener('beforeunload', (e) => {
  if (viewing) return;
  const { text } = ed.serialize();
  if (current && text !== lastSaved) { e.preventDefault(); e.returnValue = ''; }
});

async function start() {
  applyTheme(localStorage.getItem('mda.theme') === 'dark' ? 'dark' : 'light');
  applyMarking(localStorage.getItem('mda.marking') !== 'off');

  cfg = await (await api('/api/config')).json();
  $('rootName').textContent = cfg.name;
  $('rootName').title = cfg.root;
  $('whoName').textContent = cfg.user;

  await loadTree();
  listen();
  applyDiffMode(localStorage.getItem('mda.diff') || 'prev');
  toggleShelf(localStorage.getItem('mda.shelf') === 'on');

  const wanted = decodeURIComponent(location.hash.slice(1)) || localStorage.getItem(lastKey());
  if (wanted && files.some((f) => f.path === wanted)) await openFile(wanted);
  else setStatus('', 'bereit');
}

start().catch((err) => {
  setStatus('dirty', 'Fehler: ' + err.message);
});
