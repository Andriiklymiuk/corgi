package cmd

const boardPageHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="robots" content="noindex, nofollow">
<meta name="referrer" content="no-referrer">
<title>Board</title>
<style>
:root{color-scheme:light dark;--bg:#f7f7f8;--panel:#fff;--line:#e4e4e7;--text:#18181b;--dim:#71717a;--accent:#4f46e5;--warn:#b45309;--bad:#b91c1c;--ok:#15803d}
@media (prefers-color-scheme:dark){:root{--bg:#0f0f11;--panel:#18181b;--line:#27272a;--text:#f4f4f5;--dim:#a1a1aa;--accent:#818cf8;--warn:#fbbf24;--bad:#f87171;--ok:#4ade80}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,sans-serif}
header{display:flex;gap:12px;align-items:baseline;flex-wrap:wrap;padding:16px 20px;border-bottom:1px solid var(--line)}
h1{font-size:16px;margin:0;font-weight:600}
.dim{color:var(--dim);font-size:12px}
.spacer{flex:1}
input{font:inherit;color:inherit;background:var(--panel);border:1px solid var(--line);border-radius:6px;padding:5px 9px;min-width:180px}
input:focus-visible,a:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
main{display:grid;grid-auto-flow:column;grid-auto-columns:minmax(250px,1fr);gap:12px;padding:16px 20px;overflow-x:auto;align-items:start}
section{min-width:0}
h2{font-size:12px;font-weight:600;color:var(--dim);margin:0 0 8px;display:flex;justify-content:space-between}
article{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:10px 12px;margin-bottom:8px}
.top{display:flex;gap:8px;align-items:center;font-size:12px;color:var(--dim)}
.ref{font-variant-numeric:tabular-nums;color:var(--dim);text-decoration:none}
a.ref:hover,.chip:hover{color:var(--accent)}
.title{margin:4px 0 6px;font-weight:500;overflow-wrap:anywhere}
.trail{display:flex;gap:10px;flex-wrap:wrap;margin-top:6px;font-size:11px;color:var(--dim)}
.why{font-size:12px;color:var(--dim);overflow-wrap:anywhere}
.row{display:flex;gap:6px;flex-wrap:wrap;margin-top:8px}
.chip{font-family:inherit;background:none;cursor:default;font-size:11px;border:1px solid var(--line);border-radius:999px;padding:1px 8px;color:var(--dim);text-decoration:none;white-space:nowrap;max-width:100%;overflow:hidden;text-overflow:ellipsis}
button.chip{cursor:pointer;color:var(--text)}
button.chip:hover{border-color:var(--accent)}
button:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.urgent{color:var(--bad);border-color:currentColor}
.high{color:var(--warn);border-color:currentColor}
.late{color:var(--bad)}
.live{color:var(--ok)}
.empty{color:var(--dim);font-size:12px;padding:6px 2px}
#gone{padding:48px 20px;text-align:center;color:var(--dim)}
@media (max-width:700px){main{grid-auto-flow:row;grid-auto-columns:auto}}
</style>
<header>
  <h1 id="name">Board</h1>
  <span class="dim" id="meta"></span>
  <span class="spacer"></span>
  <span class="dim" id="said" role="status"></span>
  <button id="add" class="chip" type="button" hidden>Add a task</button>
  <input id="find" type="search" placeholder="Filter" aria-label="Filter cards">
</header>
<main id="board" aria-live="polite"></main>
<div id="gone" hidden></div>
<script>
(function(){
  var key=(location.hash.match(/k=([0-9a-f]{48})/)||[])[1]||'';
  var board=document.getElementById('board'),gone=document.getElementById('gone'),find=document.getElementById('find');
  var last=null,timer=0;
  function el(tag,cls,text){var n=document.createElement(tag);if(cls)n.className=cls;if(text!=null)n.textContent=text;return n}
  function web(u){try{var p=new URL(u);return p.protocol==='https:'||p.protocol==='http:'?p.href:''}catch(e){return ''}}
  function link(cls,text,u){var href=web(u);if(!href)return el('span',cls,text);var a=el('a',cls,text);a.href=href;a.target='_blank';a.rel='noopener noreferrer';return a}
  function stop(msg){board.hidden=true;gone.hidden=false;gone.textContent=msg;clearInterval(timer)}
  function card(c){
    var a=el('article'),top=el('div','top');
    top.appendChild(c.url?link('ref',c.ref,c.url):el('span','ref',c.ref));
    if(c.source&&c.source!=='link')top.appendChild(el('span','',c.source));
    if(c.running)top.appendChild(el('span','live','working now'));
    a.appendChild(top);
    a.appendChild(el('div','title',c.title||c.ref));
    if(c.why)a.appendChild(el('div','why',c.why));
    if(c.trail&&c.trail.length){
      var t=el('div','trail');
      c.trail.forEach(function(s){
        var mark={done:'\u2713',now:'\u2026',back:'\u2717',todo:'\u00b7'}[s.state]||'\u00b7';
        t.appendChild(el('span','step '+(s.state==='done'?'live':s.state==='back'?'late':''),mark+' '+s.step));
      });
      a.appendChild(t);
    }
    var row=el('div','row');
    if(c.priority)row.appendChild(el('span','chip '+(c.priority==='urgent'?'urgent':c.priority==='high'?'high':''),c.priority));
    if(c.owner)row.appendChild(el('span','chip','@'+c.owner));
    if(c.due)row.appendChild(el('span','chip'+(c.overdue?' late':''),(c.overdue?'late ':'due ')+c.due));
    (c.waitsFor||[]).forEach(function(r){row.appendChild(el('span','chip','waits for '+r))});
    if(c.idea)row.appendChild(el('span','chip','idea: '+c.idea));
    if(c.feature)row.appendChild(el('span','chip',c.feature));
    (c.links||[]).forEach(function(l){row.appendChild(link('chip',(l.source&&l.source!=='link'?l.source+' ':'')+l.label,l.url))});
    if(c.unreviewed)row.appendChild(el('span','chip late',c.unreviewed));
    if(c.triage)row.appendChild(el('span','chip','waits for a yes'));
    if(c.cycle)row.appendChild(el('span','chip',c.cycle));
    if(row.childNodes.length)a.appendChild(row);
    if(last&&last.write&&c.task){
      var acts=el('div','row');
      acts.appendChild(btn('Comment',function(){var t=window.prompt('Your comment on '+c.ref);if(t)act({do:'comment',ref:c.ref,text:t})}));
      var mine=c.owner&&c.owner.toLowerCase()===('guest:'+name()).toLowerCase();
      if(!c.owner)acts.appendChild(btn('Take it',function(){act({do:'claim',ref:c.ref})}));
      if(mine)acts.appendChild(btn('Give it back',function(){act({do:'release',ref:c.ref})}));
      a.appendChild(acts);
    }
    return a;
  }
  function btn(text,fn){var b=el('button','chip',text);b.type='button';b.addEventListener('click',fn);return b}
  function name(){
    var n='';try{n=localStorage.getItem('corgi-board-name')||''}catch(e){}
    return n;
  }
  function askName(){
    var n=name();
    if(!n){n=(window.prompt('Your name, so the owner knows who wrote')||'').trim();if(n){try{localStorage.setItem('corgi-board-name',n)}catch(e){}}}
    return n;
  }
  function act(body){
    var n=askName();if(!n)return;
    body.by=n;
    var h={'Content-Type':'application/json'};h['X-Corgi-Board-Key']=key;
    fetch('/board/act',{method:'POST',headers:h,body:JSON.stringify(body),cache:'no-store',credentials:'omit',referrerPolicy:'no-referrer'}).then(function(r){
      return r.json().then(function(j){say(r.ok?j.done:(j.error||'That did not work.'));if(r.ok)load()});
    }).catch(function(){say('The board did not answer.')});
  }
  function say(text){var m=document.getElementById('said');m.textContent=text||'';}
  function draw(){
    if(!last)return;
    var q=find.value.trim().toLowerCase();
    board.textContent='';
    document.getElementById('name').textContent=last.label||last.workspace||'Board';
    document.title=(last.label||last.workspace||'Board')+' - board';
    var meta=(last.workspace?last.workspace+' - ':'')+(last.write?'you can add, comment and take a task':'read-only');
    if(last.expires)meta+=' - link ends '+new Date(last.expires).toLocaleDateString();
    document.getElementById('meta').textContent=meta;
    document.getElementById('add').hidden=!last.write;
    (last.columns||[]).forEach(function(col){
      var cards=(last.cards||[]).filter(function(c){
        if(c.column!==col)return false;
        if(!q)return true;
        return JSON.stringify([c.ref,c.title,c.owner,c.priority,c.idea,c.feature,c.why,c.links]).toLowerCase().indexOf(q)>=0;
      });
      var s=el('section'),h=el('h2');
      h.appendChild(el('span','',col));h.appendChild(el('span','',String(cards.length)));
      s.appendChild(h);
      if(!cards.length)s.appendChild(el('div','empty','Nothing here'));
      cards.forEach(function(c){s.appendChild(card(c))});
      board.appendChild(s);
    });
  }
  function load(){
    var h={};h['X-Corgi-Board-Key']=key;
    fetch('/board/doc',{headers:h,cache:'no-store',credentials:'omit',referrerPolicy:'no-referrer'}).then(function(r){
      return r.json().then(function(j){
        if(r.status===403){stop(j.error||'This board link is closed.');return}
        if(!r.ok)return;
        last=j;draw();
      });
    }).catch(function(){});
  }
  if(!key){stop('This link has no key. Ask for the full board link.');return}
  find.addEventListener('input',draw);
  document.getElementById('add').addEventListener('click',function(){
    var t=(window.prompt('What should be done? One line.')||'').trim();if(!t)return;
    var d=window.prompt('Any detail? (optional)')||'';
    act({do:'add',title:t,text:d});
  });
  load();timer=setInterval(load,30000);
})();
</script>
</html>`
