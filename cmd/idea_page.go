package cmd

// ideaPageHTML is the idea page in the browser: the markdown rendered on
// the left, the comments on the right, a highlight on the page becoming a
// comment, the agent's answers arriving as they are written. One file, no
// build step, the dashboard's palette. It talks to /idea/<slug>/... with
// the share key from its own link, and with the dashboard's token when the
// browser has one (so Build it works for the owner without a second login).
const ideaPageHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#08090a">
<title>idea · corgi</title>
<style>
  :root{--bg:#08090a;--card:#101113;--card2:#16181b;--line:#212327;--hair:#1a1c1f;
      --text:#eceef1;--dim:#8a8f98;--dim2:#5c6169;--accent:#5e6ad2;--accent-soft:#171a2e;--accent-line:#33376b;
      --green:#3fb950;--amber:#d29922;--red:#f85149;--purple:#a371f7;--r:.5rem}
  *{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
  [hidden]{display:none!important}
  html{touch-action:manipulation;-webkit-text-size-adjust:100%}
  body{font-family:-apple-system,system-ui,sans-serif;background:var(--bg);color:var(--text);margin:0;-webkit-font-smoothing:antialiased}
  a{color:var(--accent)}
  button{font:inherit;cursor:pointer}
  header{position:sticky;top:0;z-index:30;background:rgba(8,9,10,.9);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);
      border-bottom:1px solid var(--line);padding:.7rem max(1rem,env(safe-area-inset-right)) .6rem max(1rem,env(safe-area-inset-left))}
  .head{max-width:72rem;margin:0 auto;display:flex;align-items:center;gap:.8rem;flex-wrap:wrap}
  .logo{width:1.9rem;height:1.9rem;border-radius:.6rem;background:var(--card2);border:1px solid var(--line);display:flex;align-items:center;justify-content:center;font-size:1rem;flex:0 0 auto}
  .who{min-width:0;flex:1 1 14rem}
  @media (max-width:60rem){.head{gap:.5rem}.who{flex-basis:100%;order:-1}.logo{display:none}h1{white-space:normal}}
  h1{font-size:1rem;margin:0;font-weight:600;letter-spacing:-.01em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .sub{color:var(--dim);font-size:.78rem;margin-top:.1rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .tags{display:flex;gap:.3rem;flex-wrap:wrap}
  .tag{font-size:.68rem;color:var(--dim);border:1px solid var(--line);border-radius:1rem;padding:.1rem .45rem}
  .pill{font-size:.66rem;letter-spacing:.04em;text-transform:uppercase;border-radius:.4rem;padding:.15rem .4rem;border:1px solid var(--line);color:var(--dim);white-space:nowrap}
  .pill.open{color:var(--amber);border-color:#4a3a12;background:#1d1708}
  .pill.answered{color:var(--accent);border-color:var(--accent-line);background:var(--accent-soft)}
  .pill.fixed{color:var(--green);border-color:#1f4a2a;background:#0d1f12}
  .pill.resolved{color:var(--dim2)}
  .pill.proposal{color:var(--purple);border-color:#3b2a5c;background:#17102a}
  .pill.building{color:var(--green);border-color:#1f4a2a;background:#0d1f12}
  .btn{border:1px solid var(--line);background:var(--card2);color:var(--text);border-radius:.45rem;padding:.4rem .7rem;font-size:.8rem;font-weight:500}
  .btn.primary{background:var(--accent);border-color:var(--accent);color:#fff}
  .btn.small{padding:.25rem .5rem;font-size:.72rem}
  .btn:disabled{opacity:.5;cursor:default}
  .btn.ghost{background:none;color:var(--dim)}
  .you{font-size:.76rem;color:var(--dim)}
  .you b{color:var(--text);font-weight:500;border-bottom:1px dotted var(--dim2);cursor:pointer}
  main{max-width:72rem;margin:0 auto;display:grid;grid-template-columns:minmax(0,1fr) 24rem;gap:2rem;padding:1.2rem max(1rem,env(safe-area-inset-right)) 6rem max(1rem,env(safe-area-inset-left))}
  @media (max-width:60rem){main{grid-template-columns:1fr;gap:1.2rem}}
  article{min-width:0;line-height:1.6;font-size:.95rem}
  article h1{font-size:1.5rem;margin:.2rem 0 .8rem;white-space:normal}
  article h2{font-size:1.1rem;margin:1.6rem 0 .5rem;padding-bottom:.25rem;border-bottom:1px solid var(--hair)}
  article h3{font-size:.95rem;margin:1.1rem 0 .3rem}
  article p,article li{color:#d5d8dd}
  article code{font-family:ui-monospace,Menlo,monospace;font-size:.85em;background:var(--card2);border-radius:.3rem;padding:.05rem .3rem}
  article pre{background:var(--card);border:1px solid var(--line);border-radius:var(--r);padding:.7rem .9rem;overflow:auto;font-size:.82rem}
  article pre code{background:none;padding:0}
  article blockquote{margin:.6rem 0;padding:.1rem .9rem;border-left:3px solid var(--accent-line);color:var(--dim)}
  article table{border-collapse:collapse;font-size:.86rem;margin:.6rem 0}
  article th,article td{border:1px solid var(--line);padding:.3rem .5rem;text-align:left}
  article input[type=checkbox]{margin-right:.4rem}
  article img{max-width:100%}
  mark.hl{background:#3d3410;color:inherit;border-bottom:2px solid var(--amber);border-radius:.15rem;cursor:pointer;padding:0 .05rem}
  mark.hl.answered{background:#1c1f3d;border-bottom-color:var(--accent)}
  mark.hl.fixed{background:#10281a;border-bottom-color:var(--green)}
  mark.hl.resolved{background:none;border-bottom:1px dotted var(--dim2)}
  mark.hl.proposal{background:#241a3a;border-bottom-color:var(--purple)}
  mark.hl.on{outline:2px solid var(--accent);outline-offset:1px}
  .float{position:fixed;z-index:40;transform:translate(-50%,-115%);background:var(--accent);color:#fff;border:0;border-radius:.5rem;padding:.4rem .7rem;font-size:.8rem;font-weight:600;box-shadow:0 6px 20px rgba(0,0,0,.5)}
  .endcap{margin-top:2.5rem;padding:1rem;border:1px solid var(--line);border-radius:.7rem;background:var(--card)}
  .endcap h2{margin:0 0 .3rem;border:0;padding:0;font-size:1rem}
  .endcap p{margin:.2rem 0 .7rem;color:var(--dim);font-size:.84rem}
  .tasks{list-style:none;margin:.6rem 0 0;padding:0;font-size:.84rem}
  .tasks li{display:flex;gap:.5rem;align-items:center;padding:.3rem 0;border-top:1px solid var(--hair)}
  .tasks .ref{font-family:ui-monospace,Menlo,monospace;font-size:.72rem;color:var(--dim)}
  .tasks .st{margin-left:auto;font-size:.66rem;text-transform:uppercase;letter-spacing:.04em;color:var(--dim)}
  .tasks .st.Doing{color:var(--amber)}.tasks .st.Review{color:var(--accent)}.tasks .st.Done{color:var(--green)}
  aside{min-width:0}
  @media (min-width:60.01rem){aside{position:sticky;top:4.2rem;max-height:calc(100vh - 5rem);overflow:auto;padding-right:.2rem}}
  .seg{display:flex;gap:.15rem;margin:0 0 .6rem;border:1px solid var(--line);border-radius:.5rem;padding:.15rem;background:var(--card)}
  .seg button{flex:1;border:0;background:none;color:var(--dim);font-size:.76rem;padding:.3rem .2rem;border-radius:.35rem}
  .seg button[aria-selected=true]{background:var(--card2);color:var(--text)}
  .seg .n{font-size:.66rem;color:var(--dim2);margin-left:.25rem}
  .card{border:1px solid var(--line);border-radius:.7rem;background:var(--card);padding:.7rem .8rem;margin-bottom:.6rem}
  .card.on{border-color:var(--accent)}
  .card .top{display:flex;align-items:center;gap:.4rem;font-size:.76rem;color:var(--dim);flex-wrap:wrap}
  .card .top b{color:var(--text);font-weight:600}
  .card .q{font-size:.78rem;color:var(--dim);border-left:2px solid var(--amber);padding:.1rem .5rem;margin:.4rem 0;cursor:pointer;white-space:pre-wrap}
  .card .q.proposal{border-left-color:var(--purple)}
  .card .t{font-size:.86rem;white-space:pre-wrap;margin:.3rem 0;word-break:break-word}
  .card .files{display:flex;flex-wrap:wrap;gap:.35rem;margin:.3rem 0}
  .card .files img{max-width:9rem;max-height:7rem;border-radius:.4rem;border:1px solid var(--line)}
  .card .files a{font-size:.74rem}
  .reply{margin:.5rem 0 0 .6rem;padding-left:.6rem;border-left:2px solid var(--hair)}
  .reply.agent{border-left-color:var(--accent-line)}
  .reply .top{margin-bottom:.1rem}
  .acts{display:flex;gap:.35rem;flex-wrap:wrap;margin-top:.5rem}
  textarea,input[type=text]{width:100%;font:inherit;font-size:.86rem;color:var(--text);background:var(--card2);border:1px solid var(--line);border-radius:.45rem;padding:.5rem .6rem;resize:vertical}
  textarea:focus,input:focus{outline:none;border-color:var(--accent-line)}
  .compose{border:1px solid var(--accent-line);background:var(--accent-soft);border-radius:.7rem;padding:.7rem .8rem;margin-bottom:.6rem}
  .compose .q{font-size:.78rem;color:var(--dim);border-left:2px solid var(--amber);padding:.1rem .5rem;margin:0 0 .4rem;white-space:pre-wrap;max-height:5rem;overflow:auto}
  .compose .row{display:flex;gap:.4rem;align-items:center;margin-top:.45rem;flex-wrap:wrap}
  .chip{font-size:.72rem;border:1px solid var(--line);border-radius:1rem;padding:.1rem .5rem;color:var(--dim);background:var(--card2)}
  .chip button{border:0;background:none;color:var(--dim);padding:0 0 0 .3rem}
  .empty{color:var(--dim2);font-size:.82rem;text-align:center;padding:1rem}
  .note{font-size:.78rem;color:var(--dim);margin:.3rem 0 .8rem}
  .live{display:inline-flex;align-items:center;gap:.35rem;font-size:.72rem;color:var(--dim)}
  .live i{width:.5rem;height:.5rem;border-radius:50%;background:var(--dim2);display:inline-block}
  .live.on i{background:var(--green)}
  .live.busy i{background:var(--amber);animation:pulse 1s infinite}
  @keyframes pulse{50%{opacity:.3}}
  .fab{position:fixed;right:1rem;bottom:calc(1rem + env(safe-area-inset-bottom));z-index:35}
  @media (min-width:60.01rem){.fab{display:none}}
  .toast{position:fixed;left:50%;bottom:calc(4rem + env(safe-area-inset-bottom));transform:translateX(-50%);background:var(--card2);border:1px solid var(--line);
      border-radius:.6rem;padding:.5rem .8rem;font-size:.8rem;z-index:50;box-shadow:0 6px 20px rgba(0,0,0,.5)}
  .editor{display:none}.editing .editor{display:block}.editing #doc{display:none}
  .editor textarea{min-height:60vh;font-family:ui-monospace,Menlo,monospace;font-size:.82rem}
</style>
<header>
  <div class="head">
    <div class="logo">💡</div>
    <div class="who"><h1 id="name">idea</h1><div class="sub" id="summary">loading…</div></div>
    <div class="tags" id="tags"></div>
    <span class="pill" id="status"></span>
    <span class="live" id="live"><i></i><span>live</span></span>
    <span class="you">you: <b id="you">someone</b></span>
    <button class="btn ghost small" id="editBtn" hidden>edit page</button>
    <button class="btn primary" id="buildTop">Build it</button>
  </div>
</header>
<main>
  <article>
    <div id="doc"></div>
    <div class="editor"><textarea id="md" spellcheck="false"></textarea><div class="acts"><button class="btn primary" id="saveMd">save</button><button class="btn" id="cancelMd">cancel</button></div></div>
    <div class="endcap" id="endcap">
      <h2>Hand it to the agent</h2>
      <p id="buildWhy">When the page reads right, the agent cuts it into tasks on the board, plans the order and starts building - a worktree and a pull request per task.</p>
      <div class="acts"><button class="btn primary" id="build">Build it</button><button class="btn" id="general">Comment on the whole page</button></div>
      <ul class="tasks" id="tasks"></ul>
    </div>
  </article>
  <aside>
    <div class="seg" id="seg">
      <button data-f="all" aria-selected="true">All<span class="n"></span></button>
      <button data-f="open">Open<span class="n"></span></button>
      <button data-f="proposal">Proposals<span class="n"></span></button>
      <button data-f="resolved">Resolved<span class="n"></span></button>
    </div>
    <div class="compose" id="compose" hidden>
      <div class="q" id="cq"></div>
      <textarea id="ct" rows="3" placeholder="say it plainly: what should change, or what you want to know"></textarea>
      <div class="row">
        <input type="text" id="cl" placeholder="a link (optional)" style="flex:1;min-width:8rem">
        <label class="btn small">attach<input type="file" id="cf" multiple hidden></label>
      </div>
      <div class="row" id="catt"></div>
      <div class="row"><button class="btn primary" id="csend">Comment</button><button class="btn" id="ccancel">cancel</button></div>
    </div>
    <div class="note" id="hint">Highlight words on the page to comment on them. The agent answers here and amends the page as it goes.</div>
    <div id="list"></div>
  </aside>
</main>
<button class="btn primary fab" id="fab">comments</button>
<button class="float" id="float" hidden>Comment</button>
<script>
(() => {
  const $ = s => document.querySelector(s);
  const slug = location.pathname.split('/').filter(Boolean)[1] || '';
  const key = new URLSearchParams(location.search).get('k') || '';
  const token = (() => { try { return localStorage.getItem('corgi_token') || ''; } catch { return ''; } })();
  const base = '/idea/' + encodeURIComponent(slug);
  let doc = null, filter = 'all', pending = null, attached = [], focus = 0, editing = false;

  const you = () => { try { return localStorage.getItem('corgi_idea_name') || ''; } catch { return ''; } };
  const setYou = n => { try { localStorage.setItem('corgi_idea_name', n); } catch {} $('#you').textContent = n || 'someone'; };
  const askYou = () => { const n = prompt('your name, as it appears on comments', you() || ''); if (n !== null) setYou(n.trim()); return you(); };
  $('#you').onclick = askYou;
  $('#you').textContent = you() || 'someone';

  const api = async (path, body) => {
    const h = { 'Accept': 'application/json' };
    if (token) h['Authorization'] = 'Bearer ' + token;
    const q = key ? '?k=' + encodeURIComponent(key) : '';
    const init = body ? { method: 'POST', headers: { ...h, 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : { headers: h };
    const r = await fetch(base + path + q, init);
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || r.statusText);
    return j;
  };
  const toast = (t, ms = 2600) => { const d = document.createElement('div'); d.className = 'toast'; d.textContent = t; document.body.appendChild(d); setTimeout(() => d.remove(), ms); };
  const ago = s => { const d = (Date.now() - new Date(s).getTime()) / 1000; if (d < 60) return 'now'; if (d < 3600) return Math.floor(d / 60) + 'm'; if (d < 86400) return Math.floor(d / 3600) + 'h'; return Math.floor(d / 86400) + 'd'; };
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };

  // ---- the page, and the highlights on it ---------------------------------
  const docText = () => $('#doc').textContent;
  function findQuote(root, a) {
    // Text nodes of the page in order, one string, the quote in it; the
    // candidate whose prefix and suffix match best wins, so the same words
    // twice on the page land where they were highlighted.
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const nodes = []; let text = '';
    for (let n; (n = walker.nextNode());) { nodes.push({ n, at: text.length }); text += n.nodeValue; }
    const q = a.quote; if (!q) return null;
    let best = -1, bestScore = -1;
    for (let i = text.indexOf(q); i >= 0; i = text.indexOf(q, i + 1)) {
      let s = 0;
      if (a.prefix) { const p = a.prefix.slice(-20); if (text.slice(Math.max(0, i - p.length), i) === p) s += 2; else if (text.slice(Math.max(0, i - 40), i).includes(p.slice(-8))) s += 1; }
      if (a.suffix) { const x = a.suffix.slice(0, 20); if (text.slice(i + q.length, i + q.length + x.length) === x) s += 2; }
      if (s > bestScore) { best = i; bestScore = s; }
      if (bestScore >= 4) break;
    }
    if (best < 0) return null;
    const pos = at => { for (let k = nodes.length - 1; k >= 0; k--) if (nodes[k].at <= at) return { n: nodes[k].n, o: at - nodes[k].at }; return null; };
    const s = pos(best), e = pos(best + q.length);
    if (!s || !e) return null;
    const r = document.createRange(); r.setStart(s.n, s.o); r.setEnd(e.n, Math.min(e.o, e.n.nodeValue.length)); return r;
  }
  function markRange(range, c) {
    // Wrap every text node the range touches, so a highlight across <b> or a list item still shows.
    const nodes = []; const walker = document.createTreeWalker(range.commonAncestorContainer, NodeFilter.SHOW_TEXT);
    for (let n = walker.currentNode.nodeType === 3 ? walker.currentNode : walker.nextNode(); n; n = walker.nextNode()) if (range.intersectsNode(n)) nodes.push(n);
    for (const n of nodes) {
      const r = document.createRange(); r.selectNodeContents(n);
      if (n === range.startContainer) r.setStart(n, range.startOffset);
      if (n === range.endContainer) r.setEnd(n, range.endOffset);
      if (r.collapsed) continue;
      const m = el('mark', 'hl ' + (c.kind === 'proposal' ? 'proposal ' : '') + c.status); m.dataset.id = c.id; m.title = (c.by || '') + ': ' + c.text.slice(0, 120);
      m.onclick = ev => { ev.stopPropagation(); focusOn(c.id); };
      try { r.surroundContents(m); } catch { }
    }
  }
  function renderDoc() {
    const root = $('#doc'); root.innerHTML = doc.html || '';
    root.querySelectorAll('a[href]').forEach(a => { a.target = '_blank'; a.rel = 'noopener'; });
    for (const c of doc.comments || []) {
      if (!c.anchor || (c.status === 'resolved' && filter !== 'resolved')) continue;
      const r = findQuote(root, c.anchor); if (r) markRange(r, c);
    }
    $('#md').value = doc.markdown || '';
  }

  // ---- selecting words --------------------------------------------------
  const floatBtn = $('#float');
  function selectionInDoc() {
    const sel = window.getSelection(); if (!sel || sel.isCollapsed || !sel.rangeCount) return null;
    const r = sel.getRangeAt(0); const root = $('#doc');
    if (!root.contains(r.commonAncestorContainer)) return null;
    const quote = sel.toString().replace(/\s+/g, ' ').trim(); if (quote.length < 2 || quote.length > 600) return null;
    const all = docText(); let at = -1;
    try { const pre = document.createRange(); pre.selectNodeContents(root); pre.setEnd(r.startContainer, r.startOffset); at = pre.toString().length; } catch { }
    const raw = all.slice(Math.max(0, at), Math.max(0, at) + sel.toString().length);
    const q = raw.trim() === sel.toString().trim() ? raw.trim() : quote;
    const prefix = at > 0 ? all.slice(Math.max(0, at - 40), at) : '';
    const suffix = at >= 0 ? all.slice(at + raw.length, at + raw.length + 40) : '';
    let heading = ''; let n = r.startContainer.nodeType === 3 ? r.startContainer.parentElement : r.startContainer;
    for (let e = n; e && e !== root; e = e.previousElementSibling || e.parentElement) { if (/^H[1-6]$/.test(e.tagName)) { heading = e.textContent.trim(); break; } if (e.previousElementSibling === null && e.parentElement === root) break; }
    if (!heading) { let e = n; while (e && e.parentElement !== root) e = e.parentElement; for (let p = e; p; p = p.previousElementSibling) if (/^H[1-6]$/.test(p.tagName)) { heading = p.textContent.trim(); break; } }
    return { quote: q, prefix, suffix, heading, rect: r.getBoundingClientRect() };
  }
  let selTimer = 0;
  document.addEventListener('selectionchange', () => { clearTimeout(selTimer); selTimer = setTimeout(() => {
    const s = selectionInDoc(); if (!s) { floatBtn.hidden = true; return; }
    pending = s; floatBtn.hidden = false; floatBtn.style.left = (s.rect.left + s.rect.width / 2) + 'px'; floatBtn.style.top = (s.rect.top - 6) + 'px';
  }, 120); });
  floatBtn.onclick = () => { if (!pending) return; openCompose(pending); floatBtn.hidden = true; window.getSelection().removeAllRanges(); };

  // ---- composing ----------------------------------------------------------
  function openCompose(anchor) {
    pending = anchor || null; attached = [];
    $('#compose').hidden = false; $('#cq').textContent = anchor ? '“' + anchor.quote + '”' : 'about the whole page'; $('#cq').hidden = false;
    $('#ct').value = ''; $('#cl').value = ''; renderAttached(); $('#ct').focus();
    if (window.innerWidth < 960) $('#compose').scrollIntoView({ behavior: 'smooth', block: 'center' });
  }
  function renderAttached() { const box = $('#catt'); box.innerHTML = ''; attached.forEach((f, i) => { const c = el('span', 'chip', f.name); const x = el('button', '', '×'); x.onclick = () => { attached.splice(i, 1); renderAttached(); }; c.appendChild(x); box.appendChild(c); }); }
  $('#cf').onchange = async ev => { for (const f of ev.target.files) { try { attached.push(await upload(f)); } catch (e) { toast(e.message); } } renderAttached(); ev.target.value = ''; };
  async function upload(file) {
    if (file.size > 8 * 1024 * 1024) throw new Error(file.name + ' is over 8 MB');
    const data = await new Promise((ok, no) => { const r = new FileReader(); r.onload = () => ok(String(r.result).split(',')[1] || ''); r.onerror = () => no(new Error('could not read ' + file.name)); r.readAsDataURL(file); });
    return (await api('/file', { name: file.name, type: file.type, data })).file;
  }
  $('#ccancel').onclick = () => { $('#compose').hidden = true; pending = null; };
  $('#csend').onclick = async () => {
    const text = $('#ct').value.trim(); const link = $('#cl').value.trim();
    if (!text && !attached.length && !link) { toast('say something first'); return; }
    const by = you() || askYou() || 'someone';
    const body = { text, by, links: link ? [link] : [], files: attached };
    if (pending) Object.assign(body, { quote: pending.quote, prefix: pending.prefix, suffix: pending.suffix, heading: pending.heading });
    $('#csend').disabled = true;
    try { await api('/comment', body); $('#compose').hidden = true; pending = null; toast('sent - the agent will answer here'); await load(); }
    catch (e) { toast(e.message); } finally { $('#csend').disabled = false; }
  };
  $('#general').onclick = () => openCompose(null);
  $('#fab').onclick = () => { document.querySelector('aside').scrollIntoView({ behavior: 'smooth' }); };

  // ---- the comments -----------------------------------------------------
  function focusOn(id) { focus = id; renderList(); document.querySelectorAll('mark.hl').forEach(m => m.classList.toggle('on', +m.dataset.id === id)); const c = document.getElementById('c' + id); if (c) c.scrollIntoView({ behavior: 'smooth', block: 'center' }); }
  const act = async (path, body) => { try { await api(path, body); await load(); } catch (e) { toast(e.message); } };
  function card(c) {
    const d = el('div', 'card' + (c.id === focus ? ' on' : '')); d.id = 'c' + c.id;
    const top = el('div', 'top'); top.appendChild(el('b', '', c.by)); top.appendChild(el('span', '', ago(c.at)));
    if (c.kind === 'proposal') top.appendChild(el('span', 'pill proposal', 'proposal'));
    top.appendChild(el('span', 'pill ' + c.status, c.status)); d.appendChild(top);
    if (c.anchor) { const q = el('div', 'q' + (c.kind === 'proposal' ? ' proposal' : ''), (c.anchor.heading ? c.anchor.heading + ' · ' : '') + '“' + c.anchor.quote + '”'); q.onclick = () => { const m = document.querySelector('mark.hl[data-id="' + c.id + '"]'); if (m) { m.scrollIntoView({ behavior: 'smooth', block: 'center' }); focusOn(c.id); } }; d.appendChild(q); }
    d.appendChild(el('div', 't', c.text));
    d.appendChild(filesRow(c.files, c.links));
    for (const r of c.replies || []) {
      const rd = el('div', 'reply' + (r.by === 'agent' ? ' agent' : '')); const rt = el('div', 'top'); rt.appendChild(el('b', '', r.by === 'agent' ? '🐕 agent' : r.by)); rt.appendChild(el('span', '', ago(r.at)));
      if (r.changed) rt.appendChild(el('span', 'pill fixed', 'changed the page')); rd.appendChild(rt); rd.appendChild(el('div', 't', r.text)); rd.appendChild(filesRow(r.files, r.links)); d.appendChild(rd);
    }
    const acts = el('div', 'acts');
    const replyBox = el('div'); replyBox.hidden = true; const ta = el('textarea'); ta.rows = 2; ta.placeholder = 'reply'; replyBox.appendChild(ta);
    const send = el('button', 'btn small primary', 'send'); send.onclick = () => { const t = ta.value.trim(); if (!t) return; act('/reply', { id: c.id, text: t, by: you() || askYou() || 'someone' }); };
    const rrow = el('div', 'acts'); rrow.appendChild(send); replyBox.appendChild(rrow);
    const rb = el('button', 'btn small', 'reply'); rb.onclick = () => { replyBox.hidden = !replyBox.hidden; if (!replyBox.hidden) ta.focus(); }; acts.appendChild(rb);
    if (c.kind === 'proposal' && c.status === 'open' && !(c.replies || []).some(r => r.by !== 'agent')) {
      const ok = el('button', 'btn small primary', 'accept'); ok.onclick = () => act('/reply', { id: c.id, text: 'Yes, do this.', by: you() || askYou() || 'someone' }); acts.appendChild(ok);
      const no = el('button', 'btn small', 'dismiss'); no.onclick = () => act('/status', { id: c.id, status: 'resolved' }); acts.appendChild(no);
    } else if (c.status !== 'resolved') {
      const rs = el('button', 'btn small', 'resolve'); rs.onclick = () => act('/status', { id: c.id, status: 'resolved' }); acts.appendChild(rs);
    } else {
      const ro = el('button', 'btn small', 'reopen'); ro.onclick = () => act('/status', { id: c.id, status: 'open' }); acts.appendChild(ro);
    }
    d.appendChild(acts); d.appendChild(replyBox);
    return d;
  }
  function filesRow(files, links) {
    const row = el('div', 'files');
    for (const f of files || []) { const href = base + '/file/' + encodeURIComponent(f.path.split('/').pop()) + (key ? '?k=' + encodeURIComponent(key) : ''); if ((f.type || '').startsWith('image/') && f.type !== 'image/svg+xml') { const a = el('a'); a.href = href; a.target = '_blank'; const i = el('img'); i.src = href; i.alt = f.name; a.appendChild(i); row.appendChild(a); } else { const a = el('a', '', '📎 ' + f.name); a.href = href; row.appendChild(a); } }
    for (const l of links || []) { const a = el('a', '', '🔗 ' + l.replace(/^https?:\/\//, '').slice(0, 50)); a.href = l; a.target = '_blank'; a.rel = 'noopener'; row.appendChild(a); }
    return row;
  }
  function renderList() {
    const all = doc.comments || [];
    const seen = { all: all.length, open: all.filter(c => c.status !== 'resolved').length, proposal: all.filter(c => c.kind === 'proposal' && c.status === 'open').length, resolved: all.filter(c => c.status === 'resolved').length };
    document.querySelectorAll('#seg button').forEach(b => { b.setAttribute('aria-selected', b.dataset.f === filter); b.querySelector('.n').textContent = seen[b.dataset.f] || ''; });
    const list = all.filter(c => filter === 'all' ? c.status !== 'resolved' : filter === 'open' ? (c.status === 'open' || c.status === 'answered' || c.status === 'fixed') : filter === 'proposal' ? c.kind === 'proposal' && c.status !== 'resolved' : c.status === 'resolved');
    list.sort((a, b) => new Date(b.updatedAt || b.at) - new Date(a.updatedAt || a.at));
    const box = $('#list'); box.innerHTML = '';
    if (!list.length) box.appendChild(el('div', 'empty', filter === 'all' ? 'no comments yet - highlight something on the page' : 'nothing here'));
    for (const c of list) box.appendChild(card(c));
  }
  document.querySelectorAll('#seg button').forEach(b => b.onclick = () => { filter = b.dataset.f; renderList(); renderDoc(); });

  // ---- head, tasks, build ---------------------------------------------------
  function renderHead() {
    const m = doc.idea || {};
    document.title = (m.name || m.title || 'idea') + ' · corgi';
    $('#name').textContent = m.name || m.title || slug; $('#summary').textContent = m.summary || m.title || '';
    const t = $('#tags'); t.innerHTML = ''; (m.tags || []).forEach(x => t.appendChild(el('span', 'tag', x)));
    const st = $('#status'); st.textContent = m.status || 'draft'; st.className = 'pill ' + (m.status || '');
    $('#live').classList.toggle('busy', !!m.answering); $('#live').querySelector('span').textContent = m.answering ? 'the agent is answering…' : 'live';
    const tasks = $('#tasks'); tasks.innerHTML = '';
    for (const x of doc.tasks || []) { const li = el('li'); li.appendChild(el('span', 'ref', x.ref)); li.appendChild(el('span', '', x.title)); li.appendChild(el('span', 'st ' + x.state, x.state)); tasks.appendChild(li); }
    const p = doc.plan;
    if (p) $('#buildWhy').textContent = p.ref + ' ' + p.state + ' · ' + p.done + ' done · ' + p.doing + ' doing · ' + p.review + ' in review · ' + p.todo + ' to do';
    const canBuild = !!token; $('#build').disabled = !canBuild; $('#buildTop').hidden = !canBuild;
    if (!canBuild) $('#buildWhy').textContent += ' Building needs the dashboard: open this page from corgi agent dashboard, or run corgi idea build ' + slug + '.';
    $('#editBtn').hidden = !token;
  }
  async function build() {
    const m = doc.idea || {}; let ws = m.workspace || '';
    if (!ws) { ws = prompt('which workspace builds it? (a folder corgi agent init ran in)', ''); if (!ws) return; }
    if (!confirm('Hand the page to the agent? It cuts it into tasks on the board and starts building in ' + ws + '.')) return;
    $('#build').disabled = true; $('#buildTop').disabled = true;
    try { const r = await api('/build', { workspace: ws.trim(), run: true }); toast(r.running ? 'building: ' + r.plan.goal : (r.why || 'tasks are on the board'), 6000); await load(); }
    catch (e) { toast(e.message, 6000); } finally { $('#build').disabled = false; $('#buildTop').disabled = false; }
  }
  $('#build').onclick = build; $('#buildTop').onclick = build;
  $('#editBtn').onclick = () => { editing = !editing; document.body.classList.toggle('editing', editing); $('#editBtn').textContent = editing ? 'reading' : 'edit page'; if (editing) $('#md').value = doc.markdown || ''; };
  $('#cancelMd').onclick = () => { editing = false; document.body.classList.remove('editing'); $('#editBtn').textContent = 'edit page'; };
  $('#saveMd').onclick = async () => { try { await api('/edit', { markdown: $('#md').value }); editing = false; document.body.classList.remove('editing'); $('#editBtn').textContent = 'edit page'; await load(); toast('saved'); } catch (e) { toast(e.message); } };

  // ---- loading and the stream -------------------------------------------
  let loading = null;
  async function load() {
    if (loading) return loading;
    loading = (async () => {
      try { const d = await api('/doc'); const y = window.scrollY; doc = d; renderHead(); if (!editing) renderDoc(); renderList(); window.scrollTo(0, y); $('#live').classList.add('on'); }
      catch (e) { $('#summary').textContent = e.message; $('#live').classList.remove('on'); $('#hint').textContent = e.message; }
      finally { loading = null; }
    })();
    return loading;
  }
  let es = null, retry = 1000;
  function listen() {
    if (es) es.close();
    es = new EventSource(base + '/stream' + (key ? '?k=' + encodeURIComponent(key) : ''));
    es.addEventListener('change', () => { retry = 1000; load(); });
    es.onopen = () => { retry = 1000; $('#live').classList.add('on'); };
    es.onerror = () => { $('#live').classList.remove('on'); es.close(); setTimeout(listen, retry); retry = Math.min(retry * 2, 20000); };
  }
  document.addEventListener('visibilitychange', () => { if (!document.hidden) load(); });
  load().then(listen);
})();
</script>
`
