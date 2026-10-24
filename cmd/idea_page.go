package cmd

// ideaPageHTML is the idea page in the browser: the markdown rendered on
// the left, the comments on the right, a highlight on the page becoming a
// comment, the agent's answers arriving as they are written. One file, no
// build step, the dashboard's palette. It talks to /idea/<slug>/... with
// the share key from its own link, and with the dashboard's token when the
// browser has one (so Build it and Send work for the owner without a
// second login).
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
  html{touch-action:manipulation;-webkit-text-size-adjust:100%;scroll-behavior:smooth}
  body{font-family:-apple-system,system-ui,sans-serif;background:var(--bg);color:var(--text);margin:0;-webkit-font-smoothing:antialiased}
  a{color:var(--accent)}
  button{font:inherit;cursor:pointer}
  .progress{position:fixed;top:0;left:0;right:0;height:3px;z-index:60;background:transparent}
  .progress i{display:block;height:100%;width:0;background:var(--accent);transition:width .1s linear}
  header{position:sticky;top:0;z-index:30;background:rgba(8,9,10,.9);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);
      border-bottom:1px solid var(--line);padding:.7rem max(1rem,env(safe-area-inset-right)) .6rem max(1rem,env(safe-area-inset-left))}
  .head{max-width:76rem;margin:0 auto;display:flex;align-items:center;gap:.8rem;flex-wrap:wrap}
  .logo{width:1.9rem;height:1.9rem;border-radius:.6rem;background:var(--card2);border:1px solid var(--line);display:flex;align-items:center;justify-content:center;font-size:1rem;flex:0 0 auto;text-decoration:none}
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
  .pill.building,.pill.Done,.pill.Review{color:var(--green);border-color:#1f4a2a;background:#0d1f12}
  .pill.Doing{color:var(--amber);border-color:#4a3a12;background:#1d1708}
  .pill.type{color:var(--accent);border-color:var(--accent-line)}
  .btn{border:1px solid var(--line);background:var(--card2);color:var(--text);border-radius:.45rem;padding:.4rem .7rem;font-size:.8rem;font-weight:500}
  .btn.primary{background:var(--accent);border-color:var(--accent);color:#fff}
  .btn.small{padding:.25rem .5rem;font-size:.72rem}
  .btn:disabled{opacity:.5;cursor:default}
  .btn.ghost{background:none;color:var(--dim)}
  .you{font-size:.76rem;color:var(--dim)}
  .you b{color:var(--text);font-weight:500;border-bottom:1px dotted var(--dim2);cursor:pointer}
  main{max-width:76rem;margin:0 auto;display:grid;grid-template-columns:minmax(0,1fr) 25rem;gap:2rem;padding:1.2rem max(1rem,env(safe-area-inset-right)) 6rem max(1rem,env(safe-area-inset-left))}
  @media (max-width:60rem){main{grid-template-columns:1fr;gap:1.2rem}}
  article{min-width:0;line-height:1.6;font-size:.95rem}
  article h1{font-size:1.5rem;margin:.2rem 0 .8rem;white-space:normal}
  article h2{font-size:1.1rem;margin:1.6rem 0 .5rem;padding-bottom:.25rem;border-bottom:1px solid var(--hair);position:relative}
  article h3{font-size:.95rem;margin:1.1rem 0 .3rem;position:relative}
  article h2 .secbtn,article h3 .secbtn{position:absolute;right:0;top:0;font-size:.68rem;font-weight:500;color:var(--dim);background:var(--card2);border:1px solid var(--line);border-radius:.35rem;padding:.1rem .4rem;opacity:0;transition:opacity .15s}
  article h2:hover .secbtn,article h3:hover .secbtn,article h2:focus-within .secbtn{opacity:1}
  @media (hover:none){article h2 .secbtn,article h3 .secbtn{opacity:.7}}
  article p,article li{color:#d5d8dd}
  article code{font-family:ui-monospace,Menlo,monospace;font-size:.85em;background:var(--card2);border-radius:.3rem;padding:.05rem .3rem}
  article pre{background:var(--card);border:1px solid var(--line);border-radius:var(--r);padding:.7rem .9rem;overflow:auto;font-size:.82rem}
  article pre code{background:none;padding:0}
  article blockquote{margin:.6rem 0;padding:.1rem .9rem;border-left:3px solid var(--accent-line);color:var(--dim)}
  article table{border-collapse:collapse;font-size:.86rem;margin:.6rem 0}
  article th,article td{border:1px solid var(--line);padding:.3rem .5rem;text-align:left}
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
  details.tickets{margin:.4rem 0 .8rem;border:1px solid var(--line);border-radius:.6rem;background:var(--card2)}
  details.tickets>summary{cursor:pointer;padding:.55rem .8rem;font-size:.86rem;font-weight:600;list-style:none;display:flex;align-items:center;gap:.5rem}
  details.tickets>summary::-webkit-details-marker{display:none}
  details.tickets>summary::before{content:'▸';color:var(--dim);font-size:.8rem;transition:transform .15s}
  details.tickets[open]>summary::before{transform:rotate(90deg)}
  details.tickets>summary .n{font-size:.7rem;color:var(--dim);font-weight:400}
  .tk{border-top:1px solid var(--hair);padding:.45rem .8rem;font-size:.84rem}
  .tk .row{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap}
  .tk .no{font-family:ui-monospace,Menlo,monospace;font-size:.72rem;color:var(--dim2);width:1.4rem}
  .tk .ref{font-family:ui-monospace,Menlo,monospace;font-size:.72rem;color:var(--dim)}
  .tk .ref a{color:var(--accent);text-decoration:none}
  .tk .after{font-size:.7rem;color:var(--dim2)}
  .tk .body{color:var(--dim);font-size:.8rem;white-space:pre-wrap;margin:.25rem 0 0 1.9rem;display:none}
  .tk.open .body{display:block}
  .tk .t{cursor:pointer;flex:1;min-width:8rem}
  .sent{display:flex;flex-wrap:wrap;gap:.4rem;margin:.3rem 0 .6rem;font-size:.78rem}
  .sent a{text-decoration:none;border:1px solid var(--line);border-radius:1rem;padding:.1rem .55rem;color:var(--text);background:var(--card2)}
  aside{min-width:0}
  @media (min-width:60.01rem){aside{position:sticky;top:4.2rem;max-height:calc(100vh - 5rem);overflow:auto;padding-right:.2rem}}
  details.toc{margin:0 0 .6rem;font-size:.78rem}
  details.toc>summary{cursor:pointer;color:var(--dim);font-size:.7rem;letter-spacing:.05em;text-transform:uppercase;list-style:none}
  details.toc>summary::-webkit-details-marker{display:none}
  details.toc ol{list-style:none;margin:.3rem 0 0;padding:0}
  details.toc li{display:flex;gap:.4rem;align-items:center;padding:.15rem 0;cursor:pointer;color:var(--dim)}
  details.toc li.on,details.toc li:hover{color:var(--text)}
  details.toc li .n{font-size:.66rem;color:var(--amber)}
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
  .acts{display:flex;gap:.35rem;flex-wrap:wrap;margin-top:.5rem;align-items:center}
  .quick{display:flex;gap:.35rem;align-items:flex-end;margin-top:.5rem}
  .quick textarea{flex:1;min-height:2rem;max-height:9rem;font-size:.82rem;padding:.35rem .5rem;border-radius:.4rem}
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
      border-radius:.6rem;padding:.5rem .8rem;font-size:.8rem;z-index:50;box-shadow:0 6px 20px rgba(0,0,0,.5);max-width:90vw}
  .editor{display:none}.editing .editor{display:block}.editing #doc{display:none}
  .editor textarea{min-height:60vh;font-family:ui-monospace,Menlo,monospace;font-size:.82rem}
</style>
<div class="progress"><i id="prog"></i></div>
<header>
  <div class="head">
    <a class="logo" id="home" href="/idea/" title="all pages">💡</a>
    <div class="who"><h1 id="name">idea</h1><div class="sub" id="summary">loading…</div></div>
    <span class="pill type" id="type" hidden></span>
    <div class="tags" id="tags"></div>
    <span class="pill" id="status"></span>
    <span class="live" id="live"><i></i><span>live</span></span>
    <span class="you">you: <b id="you">someone</b></span>
    <button class="btn ghost small" id="share">share link</button>
    <button class="btn ghost small" id="editBtn" hidden>edit page</button>
    <button class="btn primary" id="buildTop">Build it</button>
  </div>
</header>
<main>
  <article>
    <div id="doc"></div>
    <div class="editor"><textarea id="md" spellcheck="false"></textarea><div class="acts"><button class="btn primary" id="saveMd">save</button><button class="btn" id="cancelMd">cancel</button></div></div>
    <div class="endcap" id="endcap">
      <h2>Tickets and the build</h2>
      <p id="buildWhy">The split below is the agent's proposal of how the page becomes work; it keeps it in step with the page. When it reads right, hand it over: the tickets go on the board, a plan works through them, a worktree and a pull request per ticket.</p>
      <details class="tickets" id="tickets"><summary>Tickets <span class="n" id="tkn"></span></summary><div id="tklist"></div></details>
      <div class="sent" id="sentlist"></div>
      <div class="acts" id="buildacts"><button class="btn primary" id="build">Build it</button><span id="sendbtns"></span><button class="btn" id="general">Comment on the whole page</button></div>
      <ul class="tasks" id="tasks" hidden></ul>
    </div>
  </article>
  <aside>
    <details class="toc" id="toc" open><summary>On this page</summary><ol id="toclist"></ol></details>
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
    <div class="note" id="hint">Highlight words on the page, or hover a section heading, to comment. The agent answers here and amends the page as it goes; type under any thread to keep going.</div>
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
  const me = () => you() || askYou() || 'someone';
  $('#you').onclick = askYou;
  $('#you').textContent = you() || 'someone';
  if (!token) $('#home').removeAttribute('href');

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

  // ---- reading progress -----------------------------------------------------
  const onScroll = () => { const h = document.documentElement; const max = h.scrollHeight - h.clientHeight; $('#prog').style.width = (max > 0 ? Math.min(100, 100 * h.scrollTop / max) : 0) + '%'; markToc(); };
  document.addEventListener('scroll', onScroll, { passive: true });

  // ---- the page, and the highlights on it ---------------------------------
  const docText = () => $('#doc').textContent;
  function findQuote(root, a) {
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
  function sectionOf(h) { let t = ''; for (let e = h.nextElementSibling; e && !/^H[12]$/.test(e.tagName); e = e.nextElementSibling) t += e.textContent + ' '; return t.replace(/\s+/g, ' ').trim(); }
  function renderDoc() {
    const root = $('#doc'); root.innerHTML = doc.html || '';
    root.querySelectorAll('a[href]').forEach(a => { a.target = '_blank'; a.rel = 'noopener'; });
    for (const c of doc.comments || []) {
      if (!c.anchor || (c.status === 'resolved' && filter !== 'resolved')) continue;
      const r = findQuote(root, c.anchor); if (r) markRange(r, c);
    }
    // A "+ comment" on every section heading: faster than a highlight when the remark is about the whole section.
    const toc = $('#toclist'); toc.innerHTML = '';
    const counts = {};
    for (const c of doc.comments || []) if (c.anchor && c.anchor.heading && c.status !== 'resolved') counts[c.anchor.heading] = (counts[c.anchor.heading] || 0) + 1;
    root.querySelectorAll('h2, h3').forEach(h => {
      const title = h.textContent.trim();
      const b = el('button', 'secbtn', '+ comment'); b.title = 'comment on this section';
      b.onclick = ev => { ev.stopPropagation(); openCompose({ quote: title, prefix: '', suffix: sectionOf(h).slice(0, 40), heading: title }); };
      h.appendChild(b);
      if (h.tagName === 'H2') { const li = el('li'); li.appendChild(el('span', '', title)); if (counts[title]) li.appendChild(el('span', 'n', '● ' + counts[title])); li.onclick = () => h.scrollIntoView({ behavior: 'smooth', block: 'start' }); li.dataset.for = title; toc.appendChild(li); }
    });
    $('#toc').hidden = !toc.children.length;
    $('#md').value = doc.markdown || '';
    markToc();
  }
  function markToc() {
    const hs = [...document.querySelectorAll('#doc h2')]; if (!hs.length) return;
    let cur = hs[0]; for (const h of hs) if (h.getBoundingClientRect().top < 120) cur = h;
    const title = cur.textContent.replace('+ comment', '').trim();
    document.querySelectorAll('#toclist li').forEach(li => li.classList.toggle('on', li.dataset.for === title));
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
    let e = n; while (e && e.parentElement !== root) e = e.parentElement;
    for (let p = e; p; p = p.previousElementSibling) if (/^H[1-6]$/.test(p.tagName)) { heading = p.textContent.replace('+ comment', '').trim(); break; }
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
    $('#compose').hidden = false; $('#cq').textContent = anchor ? (anchor.heading && anchor.heading !== anchor.quote ? anchor.heading + ' · ' : '') + '“' + anchor.quote + '”' : 'about the whole page';
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
    const body = { text, by: me(), links: link ? [link] : [], files: attached };
    if (pending) Object.assign(body, { quote: pending.quote, prefix: pending.prefix, suffix: pending.suffix, heading: pending.heading });
    $('#csend').disabled = true;
    try { await api('/comment', body); $('#compose').hidden = true; pending = null; toast('sent - the agent will answer here'); await load(); }
    catch (e) { toast(e.message); } finally { $('#csend').disabled = false; }
  };
  $('#ct').addEventListener('keydown', ev => { if (ev.key === 'Enter' && (ev.metaKey || ev.ctrlKey)) $('#csend').click(); });
  $('#general').onclick = () => openCompose(null);
  $('#fab').onclick = () => { document.querySelector('aside').scrollIntoView({ behavior: 'smooth' }); };
  $('#share').onclick = async () => {
    const link = (doc && doc.link) || location.href;
    try { await navigator.clipboard.writeText(link); toast('link copied - anyone with it can read and comment'); } catch { prompt('the link', link); }
  };

  // ---- the comments -----------------------------------------------------
  function focusOn(id) { focus = id; renderList(); document.querySelectorAll('mark.hl').forEach(m => m.classList.toggle('on', +m.dataset.id === id)); const c = document.getElementById('c' + id); if (c) c.scrollIntoView({ behavior: 'smooth', block: 'center' }); }
  const act = async (path, body) => { try { await api(path, body); await load(); } catch (e) { toast(e.message); } };
  function card(c) {
    const d = el('div', 'card' + (c.id === focus ? ' on' : '')); d.id = 'c' + c.id;
    const top = el('div', 'top'); top.appendChild(el('b', '', c.by === 'agent' ? '🐕 agent' : c.by)); top.appendChild(el('span', '', ago(c.at)));
    if (c.kind === 'proposal') top.appendChild(el('span', 'pill proposal', 'proposal'));
    top.appendChild(el('span', 'pill ' + c.status, c.status)); d.appendChild(top);
    if (c.anchor) { const q = el('div', 'q' + (c.kind === 'proposal' ? ' proposal' : ''), (c.anchor.heading && c.anchor.heading !== c.anchor.quote ? c.anchor.heading + ' · ' : '') + '“' + c.anchor.quote + '”'); q.onclick = () => { const m = document.querySelector('mark.hl[data-id="' + c.id + '"]'); if (m) { m.scrollIntoView({ behavior: 'smooth', block: 'center' }); focusOn(c.id); } }; d.appendChild(q); }
    d.appendChild(el('div', 't', c.text));
    d.appendChild(filesRow(c.files, c.links));
    for (const r of c.replies || []) {
      const rd = el('div', 'reply' + (r.by === 'agent' ? ' agent' : '')); const rt = el('div', 'top'); rt.appendChild(el('b', '', r.by === 'agent' ? '🐕 agent' : r.by)); rt.appendChild(el('span', '', ago(r.at)));
      if (r.changed) rt.appendChild(el('span', 'pill fixed', 'changed the page')); rd.appendChild(rt); rd.appendChild(el('div', 't', r.text)); rd.appendChild(filesRow(r.files, r.links)); d.appendChild(rd);
    }
    const acts = el('div', 'acts');
    if (c.kind === 'proposal' && c.status === 'open' && !(c.replies || []).some(r => r.by !== 'agent')) {
      const ok = el('button', 'btn small primary', 'accept'); ok.onclick = () => act('/reply', { id: c.id, text: 'Yes, do this.', by: me() }); acts.appendChild(ok);
      const no = el('button', 'btn small', 'dismiss'); no.onclick = () => act('/status', { id: c.id, status: 'resolved' }); acts.appendChild(no);
    } else if (c.status !== 'resolved') {
      const rs = el('button', 'btn small', 'resolve'); rs.onclick = () => act('/status', { id: c.id, status: 'resolved' }); acts.appendChild(rs);
    } else {
      const ro = el('button', 'btn small', 'reopen'); ro.onclick = () => act('/status', { id: c.id, status: 'open' }); acts.appendChild(ro);
    }
    d.appendChild(acts);
    // The reply box is always there: type, Enter, done. Shift+Enter for a new line.
    const quick = el('div', 'quick'); const ta = el('textarea'); ta.rows = 1; ta.placeholder = c.status === 'resolved' ? 'say more to reopen' : 'reply…';
    const send = el('button', 'btn small primary', 'send'); send.onclick = () => { const t = ta.value.trim(); if (!t) return; ta.value = ''; act('/reply', { id: c.id, text: t, by: me() }); };
    ta.addEventListener('keydown', ev => { if (ev.key === 'Enter' && !ev.shiftKey) { ev.preventDefault(); send.click(); } });
    ta.addEventListener('input', () => { ta.style.height = 'auto'; ta.style.height = Math.min(144, ta.scrollHeight) + 'px'; });
    quick.appendChild(ta); quick.appendChild(send); d.appendChild(quick);
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
    const seen = { all: all.filter(c => c.status !== 'resolved').length, open: all.filter(c => c.status === 'open').length, proposal: all.filter(c => c.kind === 'proposal' && c.status === 'open').length, resolved: all.filter(c => c.status === 'resolved').length };
    document.querySelectorAll('#seg button').forEach(b => { b.setAttribute('aria-selected', b.dataset.f === filter); b.querySelector('.n').textContent = seen[b.dataset.f] || ''; });
    const list = all.filter(c => filter === 'all' ? c.status !== 'resolved' : filter === 'open' ? (c.status === 'open' || c.status === 'answered' || c.status === 'fixed') : filter === 'proposal' ? c.kind === 'proposal' && c.status !== 'resolved' : c.status === 'resolved');
    list.sort((a, b) => new Date(b.updatedAt || b.at) - new Date(a.updatedAt || a.at));
    const box = $('#list'); const open = new Set([...box.querySelectorAll('.quick textarea')].filter(t => t.value).map(t => t.closest('.card').id));
    const drafts = {}; box.querySelectorAll('.card').forEach(cd => { const t = cd.querySelector('.quick textarea'); if (t && t.value) drafts[cd.id] = t.value; });
    box.innerHTML = '';
    if (!list.length) box.appendChild(el('div', 'empty', filter === 'all' ? 'no comments yet - highlight something on the page' : 'nothing here'));
    for (const c of list) { const cd = card(c); box.appendChild(cd); if (drafts[cd.id]) cd.querySelector('.quick textarea').value = drafts[cd.id]; }
    void open;
  }
  document.querySelectorAll('#seg button').forEach(b => b.onclick = () => { filter = b.dataset.f; renderList(); renderDoc(); });

  // ---- head, tickets, build, send -------------------------------------------
  function renderHead() {
    const m = doc.idea || {};
    document.title = (m.name || m.title || 'idea') + ' · corgi';
    $('#name').textContent = m.name || m.title || slug; $('#summary').textContent = m.summary || m.title || '';
    const ty = $('#type'); ty.textContent = m.type || 'idea'; ty.hidden = !m.type;
    const t = $('#tags'); t.innerHTML = ''; (m.tags || []).forEach(x => t.appendChild(el('span', 'tag', x)));
    const st = $('#status'); st.textContent = m.status || 'draft'; st.className = 'pill ' + (m.status || '');
    $('#live').classList.toggle('busy', !!m.answering); $('#live').querySelector('span').textContent = m.answering ? 'the agent is answering…' : 'live';
    // Tickets: the split, collapsed; a ticket opens on tap to show its body.
    const tk = $('#tklist'); tk.innerHTML = '';
    const tickets = doc.tickets || [];
    $('#tkn').textContent = tickets.length ? tickets.length + (tickets.some(x => x.ref) ? ' · ' + tickets.filter(x => x.state === 'Done').length + ' done' : ' proposed') : 'none yet';
    for (const x of tickets) {
      const d = el('div', 'tk'); const row = el('div', 'row'); row.appendChild(el('span', 'no', x.id + '.'));
      const title = el('span', 't', x.title); title.onclick = () => d.classList.toggle('open'); row.appendChild(title);
      if (x.ref) { const ref = el('span', 'ref'); if (x.url) { const a = el('a', '', x.ref); a.href = x.url; a.target = '_blank'; a.rel = 'noopener'; ref.appendChild(a); } else ref.textContent = x.ref; row.appendChild(ref); }
      if (x.state) row.appendChild(el('span', 'pill ' + x.state, x.state));
      if ((x.after || []).length) row.appendChild(el('span', 'after', 'after ' + x.after.join(', ')));
      const cb = el('button', 'btn small ghost', '+ comment'); cb.title = 'comment on this ticket'; cb.onclick = ev => { ev.stopPropagation(); openCompose({ quote: x.title, prefix: '', suffix: '', heading: 'Tickets' }); }; row.appendChild(cb);
      d.appendChild(row); if (x.body) d.appendChild(el('div', 'body', x.body)); tk.appendChild(d);
    }
    const sent = $('#sentlist'); sent.innerHTML = '';
    for (const l of doc.sent || []) { const a = el('a', '', (l.kind === 'page' ? '📄 ' : '🎯 ') + l.ref + (l.title ? ' · ' + l.title : '')); a.href = l.url; a.target = '_blank'; a.rel = 'noopener'; sent.appendChild(a); }
    const p = doc.plan;
    const canBuild = !!token; $('#build').disabled = !canBuild; $('#buildTop').hidden = !canBuild;
    let why = 'The split below is the agent\'s proposal of how the page becomes work; it keeps it in step with the page. When it reads right, hand it over: the tickets go on the board, a plan works through them, a worktree and a pull request per ticket.';
    if (p) why = p.ref + ' ' + p.state + ' · ' + p.done + ' done · ' + p.doing + ' doing · ' + p.review + ' in review · ' + p.todo + ' to do';
    else if (!canBuild) why += ' Building and sending need the dashboard: open this page from corgi agent dashboard, or run corgi idea build ' + slug + '.';
    $('#buildWhy').textContent = why;
    $('#editBtn').hidden = !token;
    const sb = $('#sendbtns'); sb.innerHTML = '';
    if (token) for (const to of ['linear', 'jira', 'confluence']) { const opt = (doc.send || {})[to]; if (!opt) continue; const b = el('button', 'btn', 'send to ' + to); b.onclick = () => send(to, opt); sb.appendChild(b); }
  }
  async function build() {
    const m = doc.idea || {}; let ws = m.workspace || '';
    if (!ws) { ws = prompt('which workspace builds it? (a folder corgi agent init ran in)', ''); if (!ws) return; }
    if (!confirm('Hand the page to the agent? Its ' + ((doc.tickets || []).length || 'proposed') + ' tickets go on the board and the build starts in ' + ws + '.')) return;
    $('#build').disabled = true; $('#buildTop').disabled = true;
    try { const r = await api('/build', { workspace: ws.trim(), run: true }); toast(r.running ? 'building: ' + r.plan.goal : (r.why || 'tasks are on the board'), 6000); await load(); }
    catch (e) { toast(e.message, 6000); } finally { $('#build').disabled = false; $('#buildTop').disabled = false; }
  }
  async function send(to, opt) {
    let project = opt.project || opt.team || '', space = '';
    if (to === 'confluence') { space = prompt('the Confluence space key', ''); if (!space) return; }
    else if (!project) { project = prompt(to === 'jira' ? 'the Jira project key' : 'the Linear team key (as in ABC-123)', ''); if (!project) return; }
    if (!confirm('Send to ' + to + '? ' + (to === 'confluence' ? 'The page goes up as it is.' : 'An epic and ' + (doc.tickets || []).length + ' ticket(s) are created in ' + project + '.'))) return;
    try { const r = await api('/send', { to, workspace: project, space }); toast('sent: ' + (r.sent[0] || {}).url, 7000); await load(); } catch (e) { toast(e.message, 7000); }
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

// ideaIndexHTML is the menu of every page, for the dashboard's own browser
// (it needs the dashboard's token; a share key opens one page, not the list).
const ideaIndexHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<title>pages · corgi</title>
<style>
  :root{--bg:#08090a;--card:#101113;--card2:#16181b;--line:#212327;--text:#eceef1;--dim:#8a8f98;--dim2:#5c6169;--accent:#5e6ad2;--amber:#d29922;--green:#3fb950}
  body{font-family:-apple-system,system-ui,sans-serif;background:var(--bg);color:var(--text);margin:0;-webkit-font-smoothing:antialiased}
  main{max-width:46rem;margin:0 auto;padding:1.2rem 1rem 4rem}
  h1{font-size:1.1rem;margin:.2rem 0 .2rem}
  .sub{color:var(--dim);font-size:.8rem;margin:0 0 1rem}
  .row{display:block;text-decoration:none;color:inherit;border:1px solid var(--line);border-radius:.7rem;background:var(--card);padding:.7rem .9rem;margin-bottom:.5rem}
  .row:hover{border-color:var(--accent)}
  .row b{font-weight:600}
  .row .s{color:var(--dim);font-size:.8rem;margin-top:.15rem}
  .row .m{display:flex;gap:.5rem;flex-wrap:wrap;margin-top:.35rem;font-size:.7rem;color:var(--dim)}
  .pill{letter-spacing:.04em;text-transform:uppercase;border:1px solid var(--line);border-radius:.4rem;padding:.1rem .4rem}
  .pill.type{color:var(--accent)}
  .n{color:var(--amber)}
  .empty,.err{color:var(--dim2);text-align:center;padding:2rem 0}
  code{font-family:ui-monospace,Menlo,monospace;font-size:.85em;background:var(--card2);padding:.05rem .3rem;border-radius:.3rem}
  .copy{float:right;border:1px solid var(--line);background:var(--card2);color:var(--dim);border-radius:.4rem;padding:.15rem .45rem;font:inherit;font-size:.7rem;cursor:pointer}
</style>
<main>
  <h1>💡 Pages</h1>
  <p class="sub">Ideas, specs, designs and proposals the agent keeps. <code>corgi idea new "…"</code> adds one.</p>
  <div id="list" class="empty">loading…</div>
</main>
<script>
(() => {
  const token = (() => { try { return localStorage.getItem('corgi_token') || ''; } catch { return ''; } })();
  const list = document.getElementById('list');
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
  (async () => {
    try {
      const r = await fetch('/idea/', { headers: { Accept: 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) } });
      const j = await r.json(); if (!r.ok) throw new Error(j.error || r.statusText);
      list.className = ''; list.innerHTML = '';
      if (!(j.ideas || []).length) { list.className = 'empty'; list.textContent = 'no pages yet'; return; }
      for (const m of j.ideas) {
        const a = el('a', 'row'); a.href = '/idea/' + encodeURIComponent(m.slug);
        if (m.link) { const c = el('button', 'copy', 'copy share link'); c.onclick = async ev => { ev.preventDefault(); try { await navigator.clipboard.writeText(m.link); c.textContent = 'copied'; } catch { prompt('the link', m.link); } }; a.appendChild(c); }
        a.appendChild(el('b', '', m.name || m.title)); a.appendChild(el('div', 's', m.summary || m.title));
        const meta = el('div', 'm'); meta.appendChild(el('span', 'pill type', m.type || 'idea')); meta.appendChild(el('span', 'pill', m.status || 'draft'));
        meta.appendChild(el('span', '', (m.open || 0) + ' open')); if (m.forAgent) meta.appendChild(el('span', 'n', m.forAgent + ' waiting on the agent'));
        if (m.workspace) meta.appendChild(el('span', '', m.workspace)); (m.tags || []).forEach(t => meta.appendChild(el('span', '', '#' + t)));
        a.appendChild(meta); list.appendChild(a);
      }
    } catch (e) { list.className = 'err'; list.textContent = e.message + (token ? '' : ' - open this from corgi agent dashboard'); }
  })();
})();
</script>
`
