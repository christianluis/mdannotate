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

let cfg = {};
let current = null;
let mod = 0;
let lastSaved = null;
let saving = false;
let pending = false;
let conflicted = false;
let saveTimer = 0;
let files = [];
const collapsed = new Set();

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

function showBanner(text, actionLabel, action) {
  bannerText.textContent = text;
  bannerAction.textContent = actionLabel;
  bannerAction.onclick = action;
  bannerEl.hidden = false;
}

function hideBanner() {
  bannerEl.hidden = true;
  bannerEl.querySelector('.keep')?.remove();
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
  if (!current) return;
  setStatus('dirty', 'ungesichert');
  clearTimeout(saveTimer);
  saveTimer = setTimeout(save, 900);
}

async function save(force = false) {
  if (!current) return;
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
      body: JSON.stringify({ text, mod: force ? 0 : mod }),
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
  b.className = 'link-btn keep';
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
  if (!current || conflicted) return;
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
  if (mod2 && e.key.toLowerCase() === 's') { e.preventDefault(); clearTimeout(saveTimer); save(); }
  if (mod2 && e.key.toLowerCase() === 'p') { e.preventDefault(); filterEl.focus(); filterEl.select(); }
  if (e.key === 'Escape' && document.activeElement === filterEl) {
    filterEl.value = '';
    if (treeRoot) drawTree(treeRoot);
    ed.focus();
  }
});

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') flush();
});

window.addEventListener('beforeunload', (e) => {
  const { text } = ed.serialize();
  if (current && text !== lastSaved) { e.preventDefault(); e.returnValue = ''; }
});

async function start() {
  applyTheme(localStorage.getItem('mda.theme') === 'dark' ? 'dark' : 'light');

  cfg = await (await api('/api/config')).json();
  $('rootName').textContent = cfg.name;
  $('rootName').title = cfg.root;
  $('whoName').textContent = cfg.user;

  await loadTree();
  listen();

  const wanted = decodeURIComponent(location.hash.slice(1)) || localStorage.getItem(lastKey());
  if (wanted && files.some((f) => f.path === wanted)) await openFile(wanted);
  else setStatus('', 'bereit');
}

start().catch((err) => {
  setStatus('dirty', 'Fehler: ' + err.message);
});
