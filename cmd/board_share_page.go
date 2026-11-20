package cmd

const boardPageHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="robots" content="noindex, nofollow">
<meta name="referrer" content="no-referrer">
<title>Board</title>
<style>
:root{color-scheme:light dark;--bg:#f7f7f8;--panel:#fff;--line:#e4e4e7;--text:#18181b;--dim:#6b6b74;--accent:#4f46e5;--warn:#a34a05;--bad:#b91c1c;--ok:#15803d}
@media (prefers-color-scheme:dark){:root{--bg:#0f0f11;--panel:#18181b;--line:#27272a;--text:#f4f4f5;--dim:#a1a1aa;--accent:#a5b4fc;--warn:#fbbf24;--bad:#f87171;--ok:#4ade80}}
*{box-sizing:border-box}
[hidden]{display:none!important}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,sans-serif}
header{display:flex;gap:12px;align-items:center;flex-wrap:wrap;padding:12px 24px;border-bottom:1px solid var(--line)}
h1{font-size:16px;margin:0;font-weight:600}
.dim{color:var(--dim);font-size:12px}
.spacer{flex:1}
input,textarea{font:inherit;color:inherit;background:var(--panel);border:1px solid var(--line);border-radius:6px;padding:6px 8px}
#find{min-width:160px}
textarea{width:100%;min-height:96px;resize:vertical;display:block}
button{font:inherit;cursor:pointer;border-radius:6px;padding:6px 12px;border:1px solid var(--line);background:none;color:var(--text)}
button.primary{background:var(--accent);border-color:var(--accent);color:var(--bg);font-weight:600}
button.text{border:0;padding:4px 0;color:var(--accent);font-size:12px}
button:disabled{opacity:.5;cursor:default}
:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.seg{display:flex;gap:16px}
.seg button{border:0;padding:4px 0;color:var(--dim);border-radius:0}
.seg button[aria-pressed=true]{color:var(--text);font-weight:600}
main{padding:16px 24px 32px}
main.cols{display:grid;grid-auto-flow:column;grid-auto-columns:minmax(220px,1fr);gap:24px;overflow-x:auto;align-items:start}
section{min-width:0}
h2{font-size:13px;font-weight:600;margin:0 0 4px;display:flex;gap:8px;align-items:baseline}
h2 .n{color:var(--dim);font-weight:400;font-variant-numeric:tabular-nums}
h2 .sum{color:var(--dim);font-weight:400;font-size:12px}
.card{padding:12px 0;border-top:1px solid var(--line)}
.title{font-weight:500;overflow-wrap:anywhere}
.meta,.why,.links,.trail{font-size:12px;color:var(--dim);margin-top:4px;overflow-wrap:anywhere}
.meta span+span::before,.links a+a::before,.links span+span::before,.links a+span::before,.links span+a::before{content:"\00b7";margin:0 6px;color:var(--dim)}
.links a,a.ref{color:inherit;text-decoration:underline;text-decoration-color:var(--line);text-underline-offset:2px}
.links a:hover,a.ref:hover{color:var(--accent)}
.trail{display:flex;gap:12px;flex-wrap:wrap}
.acts{display:flex;gap:16px;margin-top:4px}
.bad{color:var(--bad)}
.warn{color:var(--warn)}
.ok{color:var(--ok)}
.feature{margin-bottom:32px;max-width:760px}
.note{color:var(--dim);padding:48px 24px;text-align:center}
.note b{display:block;color:var(--text);font-weight:600;margin-bottom:4px}
#sheet{position:fixed;inset:0;background:rgba(0,0,0,.4);display:flex;align-items:flex-end;justify-content:center;padding:16px}
#sheet .box{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:16px;width:min(480px,100%)}
#sheet label{display:block;font-size:12px;color:var(--dim);margin:12px 0 4px}
#sheet label:first-of-type{margin-top:8px}
#sheet input{width:100%}
#sheet .row{display:flex;gap:8px;justify-content:flex-end;margin-top:16px;align-items:center}
#sheet .err{color:var(--bad);font-size:12px;margin-right:auto}
@media (min-width:701px){#sheet{align-items:center}}
@media (max-width:700px){main.cols{grid-auto-flow:row;grid-auto-columns:auto;gap:24px}header,main{padding-left:16px;padding-right:16px}}
@media (prefers-reduced-motion:no-preference){button{transition:opacity .1s}}
</style>
<header>
  <h1 id="name">Board</h1>
  <span class="dim" id="meta"></span>
  <span class="spacer"></span>
  <span class="dim" id="said" role="status"></span>
  <div class="seg" id="seg" role="group" aria-label="Group cards">
    <button type="button" data-view="status" aria-pressed="true">By status</button>
    <button type="button" data-view="feature" aria-pressed="false">By feature</button>
  </div>
  <input id="find" type="search" placeholder="Filter" aria-label="Filter cards">
  <button id="add" class="primary" type="button" hidden>Add a task</button>
</header>
<main id="board" class="cols" aria-live="polite"></main>
<div id="gone" class="note" hidden></div>
<div id="sheet" hidden>
  <div class="box" role="dialog" aria-modal="true" aria-labelledby="sheetTitle">
    <b id="sheetTitle"></b>
    <label for="who">Your name, so the owner knows who wrote</label>
    <input id="who" autocomplete="name" maxlength="60">
    <label for="line" id="lineLabel">What should be done? One line</label>
    <input id="line" maxlength="200">
    <label for="words" id="wordsLabel">Detail</label>
    <textarea id="words" maxlength="8000"></textarea>
    <div class="row"><span class="err" id="err" role="alert"></span><button type="button" id="cancel">Cancel</button><button type="button" class="primary" id="send">Send</button></div>
  </div>
</div>
<script>
(function(){
  var key=(location.hash.match(/k=([0-9a-f]{48})/)||[])[1]||'';
  var $=function(id){return document.getElementById(id)};
  var board=$('board'),gone=$('gone'),find=$('find'),sheet=$('sheet');
  var last=null,timer=0,view=/[#&]v=feature\b/.test(location.hash)?'feature':'status',fresh=0,pending=null;
  var restates={Inbox:'waiting in the inbox',Running:'picked up',Review:'in review',Done:'done'};
  var doneColumn='Done';
  function el(tag,cls,text){var n=document.createElement(tag);if(cls)n.className=cls;if(text!=null)n.textContent=text;return n}
  function web(u){try{var p=new URL(u);return p.protocol==='https:'||p.protocol==='http:'?p.href:''}catch(e){return ''}}
  function link(cls,text,u){var href=web(u);if(!href)return el('span',cls,text);var a=el('a',cls,text);a.href=href;a.target='_blank';a.rel='noopener noreferrer';return a}
  function stop(title,msg){board.hidden=true;gone.hidden=false;gone.textContent='';gone.appendChild(el('b','',title));gone.appendChild(document.createTextNode(msg));clearInterval(timer)}
  function name(){try{return localStorage.getItem('corgi-board-name')||''}catch(e){return ''}}
  function say(text){$('said').textContent=text||''}
  function textButton(text,fn){var b=el('button','text',text);b.type='button';b.addEventListener('click',fn);return b}
  function card(c,withColumn){
    var a=el('article','card');
    a.appendChild(el('div','title',c.title||c.ref));
    var meta=el('div','meta');
    meta.appendChild(c.url?link('ref',c.ref,c.url):el('span','',c.ref));
    if(withColumn)meta.appendChild(el('span','',c.column.toLowerCase()));
    if(c.running)meta.appendChild(el('span','ok','working now'));
    if(c.owner)meta.appendChild(el('span','',c.owner));
    if(c.priority==='urgent'||c.priority==='high')meta.appendChild(el('span',c.priority==='urgent'?'bad':'warn',c.priority));
    if((c.touches||[]).length)meta.appendChild(el('span','',c.touches.join(' + ')));
    if(c.due)meta.appendChild(el('span',c.overdue?'bad':'',(c.overdue?'late, due ':'due ')+c.due));
    if(c.triage)meta.appendChild(el('span','warn','waits for a yes'));
    if(c.unreviewed)meta.appendChild(el('span','warn',c.unreviewed));
    if(c.cycle)meta.appendChild(el('span','',c.cycle));
    if(view==='status'&&(c.idea||c.feature))meta.appendChild(el('span','',c.idea||c.feature));
    a.appendChild(meta);
    if(c.why&&c.why!==restates[c.column])a.appendChild(el('div','why',c.why));
    if((c.links||[]).length){
      var links=el('div','links');
      c.links.forEach(function(l){links.appendChild(link('',l.label,l.url))});
      a.appendChild(links);
    }
    if(c.trail&&c.trail.length&&c.column!==doneColumn){
      var t=el('div','trail');
      c.trail.forEach(function(s){
        var mark={done:'✓',now:'…',back:'✗',todo:'·'}[s.state]||'·';
        t.appendChild(el('span',s.state==='back'?'bad':'',mark+' '+s.step));
      });
      a.appendChild(t);
    }
    if(last.write&&c.task){
      var acts=el('div','acts');
      acts.appendChild(textButton('Comment',function(){openSheet({do:'comment',ref:c.ref},'Comment on '+c.ref)}));
      var mine=c.owner&&c.owner.toLowerCase()===('guest:'+name()).toLowerCase();
      if(!c.owner&&c.column!==doneColumn)acts.appendChild(textButton('Take it',function(){quick({do:'claim',ref:c.ref},'Take '+c.ref)}));
      if(mine)acts.appendChild(textButton('Give it back',function(){quick({do:'release',ref:c.ref},'Give '+c.ref+' back')}));
      a.appendChild(acts);
    }
    return a;
  }
  function openSheet(body,title){
    pending=body;
    $('sheetTitle').textContent=title;
    $('who').value=name();
    var adding=body.do==='add',writing=adding||body.do==='comment';
    $('line').hidden=$('lineLabel').hidden=!adding;
    $('words').hidden=$('wordsLabel').hidden=!writing;
    $('wordsLabel').textContent=adding?'Detail, if any':'Your comment';
    $('line').value='';$('words').value='';$('err').textContent='';$('send').disabled=false;
    $('send').textContent=adding?'Add the task':body.do==='comment'?'Send':title.split(' ')[0];
    sheet.hidden=false;
    (name()?(adding?$('line'):writing?$('words'):$('send')):$('who')).focus();
  }
  function quick(body,title){openSheet(body,title);if(name())send()}
  function closeSheet(){sheet.hidden=true;pending=null}
  function send(){
    var who=$('who').value.trim(),body=pending;
    if(!body)return;
    if(!who){$('err').textContent='Write your name first.';$('who').focus();return}
    if(body.do==='add'){body.title=$('line').value.trim();body.text=$('words').value;if(!body.title){$('err').textContent='Say what should be done.';$('line').focus();return}}
    if(body.do==='comment'){body.text=$('words').value.trim();if(!body.text){$('err').textContent='Write the comment first.';$('words').focus();return}}
    try{localStorage.setItem('corgi-board-name',who)}catch(e){}
    body.by=who;
    $('send').disabled=true;$('err').textContent='';
    var h={'Content-Type':'application/json'};h['X-Corgi-Board-Key']=key;
    fetch('/board/act',{method:'POST',headers:h,body:JSON.stringify(body),cache:'no-store',credentials:'omit',referrerPolicy:'no-referrer'}).then(function(r){
      return r.json().then(function(j){
        if(!r.ok){$('err').textContent=j.error||'The board refused that. Your words are kept here.';$('send').disabled=false;return}
        closeSheet();say(j.done);load();
      });
    }).catch(function(){$('err').textContent='The board did not answer. Your words are kept here; try again.';$('send').disabled=false});
  }
  function matches(c,q){
    return !q||JSON.stringify([c.ref,c.title,c.owner,c.priority,c.idea,c.feature,c.why,c.links,c.touches,c.column]).toLowerCase().indexOf(q)>=0;
  }
  function empty(q){
    var n=el('div','note');
    if(q){n.appendChild(el('b','','No card matches "'+q+'"'));n.appendChild(document.createTextNode('Clear the filter to see every card.'));return n}
    n.appendChild(el('b','','No cards yet'));
    n.appendChild(document.createTextNode('Tasks, tickets and pull requests the owner works on show up here.'+(last.write?' Add a task to put the first one on the board.':'')));
    return n;
  }
  function drawStatus(cards){
    board.className='cols';
    (last.columns||[]).forEach(function(col){
      var mine=cards.filter(function(c){return c.column===col});
      var s=el('section'),h=el('h2');
      h.appendChild(el('span','',col));h.appendChild(el('span','n',String(mine.length)));
      s.appendChild(h);
      mine.forEach(function(c){s.appendChild(card(c,false))});
      board.appendChild(s);
    });
  }
  function drawFeatures(cards){
    board.className='';
    var order=[],groups={};
    cards.forEach(function(c){
      var k=c.idea||c.feature||'';
      if(!groups[k]){groups[k]=[];order.push(k)}
      groups[k].push(c);
    });
    order.sort(function(a,b){return (a==='')-(b==='')});
    order.forEach(function(k){
      var mine=groups[k],done=0,review=0,running=0;
      mine.forEach(function(c){if(c.column===doneColumn)done++;else if(c.column==='Review')review++;else if(c.column==='Running')running++});
      var parts=[done+' of '+mine.length+' done'];
      if(review)parts.push(review+' in review');
      if(running)parts.push(running+' running');
      var s=el('section','feature'),h=el('h2');
      h.appendChild(el('span','',k||'Not part of a feature'));h.appendChild(el('span','sum',parts.join(' · ')));
      s.appendChild(h);
      mine.forEach(function(c){s.appendChild(card(c,true))});
      board.appendChild(s);
    });
  }
  function draw(){
    if(!last)return;
    var q=find.value.trim().toLowerCase();
    board.textContent='';
    var title=last.label||last.workspace||'Board';
    $('name').textContent=title;
    document.title=title+' - board';
    var meta=[];
    if(last.workspace&&last.workspace!==title)meta.push(last.workspace);
    meta.push(last.write?'You can add, comment and take a task':'Read-only');
    if(last.expires)meta.push('link ends '+new Date(last.expires).toLocaleDateString());
    $('meta').textContent=meta.join(' · ');
    $('add').hidden=!last.write;
    var cards=(last.cards||[]).filter(function(c){return matches(c,q)});
    if(!cards.length){board.className='';board.appendChild(empty(q));return}
    if(view==='feature')drawFeatures(cards);else drawStatus(cards);
  }
  function load(){
    var h={};h['X-Corgi-Board-Key']=key;
    fetch('/board/doc',{headers:h,cache:'no-store',credentials:'omit',referrerPolicy:'no-referrer'}).then(function(r){
      return r.json().then(function(j){
        if(r.status===403){stop('This board link is closed',j.error||'Ask the owner for a new link.');return}
        if(!r.ok){stale();return}
        last=j;fresh=Date.now();say('');draw();
      });
    }).catch(stale);
  }
  function stale(){
    if(!last){say('The board does not answer. Trying again.');return}
    say('Not updated since '+new Date(fresh).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})+'. Trying again.');
  }
  if(!key){stop('This link has no key','Ask for the full board link.');return}
  find.addEventListener('input',draw);
  $('seg').addEventListener('click',function(e){
    var b=e.target.closest('button');if(!b)return;
    view=b.dataset.view;
    Array.prototype.forEach.call($('seg').children,function(x){x.setAttribute('aria-pressed',String(x===b))});
    draw();
  });
  $('add').addEventListener('click',function(){openSheet({do:'add'},'Add a task')});
  $('cancel').addEventListener('click',closeSheet);
  $('send').addEventListener('click',send);
  sheet.addEventListener('click',function(e){if(e.target===sheet)closeSheet()});
  document.addEventListener('keydown',function(e){if(e.key==='Escape'&&!sheet.hidden)closeSheet()});
  Array.prototype.forEach.call($('seg').children,function(x){x.setAttribute('aria-pressed',String(x.dataset.view===view))});
  setTimeout(function(){if(!last&&gone.hidden)say('Loading the board…')},1000);
  load();timer=setInterval(load,30000);
})();
</script>
</html>`
