package serve

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>legwork serve</title>
<style>
:root{color-scheme:dark;--bg:#080a0e;--panel:#10151d;--panel2:#151c27;--line:#283447;--text:#eef3fb;--muted:#9aa9bd;--good:#5fd18f;--warn:#e8b45d;--bad:#ff7373;--info:#7fb5ff;--ink:#05070a;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text)}button,select{font:inherit}
.shell{max-width:1680px;margin:0 auto;padding:16px}.top{display:grid;grid-template-columns:1fr auto;gap:14px;align-items:start;margin-bottom:14px}
.brand{border:1px solid var(--line);background:linear-gradient(180deg,#121923,#0d1118);border-radius:8px;padding:14px 16px}.brand h1{margin:0;font-size:22px;letter-spacing:0}.sub{color:var(--muted);font-size:13px;margin-top:6px}
.indicators{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}.pill{border:1px solid var(--line);border-radius:999px;padding:6px 9px;color:var(--muted);font-size:12px;background:#0e141c}.pill.good{color:var(--good);border-color:#2e6b4c}.pill.live{color:var(--info);border-color:#315f99}.pill.warn{color:var(--warn);border-color:#7a5b2a}
.grid{display:grid;grid-template-columns:280px minmax(520px,1fr)380px;gap:14px}.panel{border:1px solid var(--line);background:var(--panel);border-radius:8px;overflow:hidden;min-width:0}.head{display:flex;justify-content:space-between;gap:10px;align-items:center;padding:11px 12px;border-bottom:1px solid var(--line);background:var(--panel2)}.title{font-size:12px;font-weight:800;text-transform:uppercase;letter-spacing:.08em;color:#c8d3e2}.body{padding:12px}
.runs{display:grid;gap:8px}.run{width:100%;text-align:left;border:1px solid var(--line);background:#0d1219;color:var(--text);border-radius:8px;padding:10px;cursor:pointer}.run.active{border-color:var(--info);background:#102033}.run-name{font-weight:800;overflow-wrap:anywhere}.run-meta,.note{font-size:12px;color:var(--muted);margin-top:5px}.note{line-height:1.35}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-bottom:12px}.stat{border:1px solid var(--line);border-radius:8px;background:#0d1219;padding:10px}.num{font-size:24px;font-weight:900}.label{font-size:12px;color:var(--muted);margin-top:2px}
.attention{display:grid;gap:8px;margin-bottom:12px}.attn{border:1px solid #654630;border-left:4px solid var(--warn);background:#17130d;border-radius:8px;padding:9px}.attn.urgent{border-color:#6d3838;border-left-color:var(--bad);background:#1a1112}.attn strong{font-size:13px}.attn p{margin:4px 0 0;color:#d8c8b0;font-size:12px;line-height:1.35}
table{width:100%;border-collapse:collapse}th{font-size:11px;text-align:left;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);border-bottom:1px solid var(--line);padding:8px}td{border-bottom:1px solid #202b3a;padding:9px 8px;vertical-align:top;font-size:13px}tr{cursor:pointer}tr:hover,tr.selected{background:#122033}.jobid{font-weight:900}.task{max-width:360px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;color:#d2dceb}.state{font-weight:800}.state.needs-input,.state.failed,.state.blocked,.state.auth-required{color:var(--bad)}.state.interrupted,.ctxhigh{color:var(--warn)}.state.done{color:var(--good)}
.timeline{display:grid;gap:8px;max-height:390px;overflow:auto}.event{display:grid;grid-template-columns:72px 92px 1fr;gap:8px;border:1px solid #202b3a;border-radius:8px;background:#0d1219;padding:8px}.time,.src{font-size:12px;color:var(--muted)}.etype{font-size:12px;font-weight:900;color:var(--info)}.preview{font-size:13px;line-height:1.35;overflow-wrap:anywhere;color:#d4deec}
.detail h2{margin:0 0 8px;font-size:22px}.kv{display:grid;grid-template-columns:88px 1fr;gap:7px 10px;font-size:13px}.kv div:nth-child(odd){color:var(--muted)}.question{margin-top:12px;border:1px dashed #74522a;background:#181309;border-radius:8px;padding:10px;color:#f4cf91}.disabled{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:12px}.disabled button{border:1px solid var(--line);background:#0d1219;color:var(--muted);border-radius:8px;padding:9px;cursor:not-allowed}.placeholder{margin-top:12px;border:1px solid var(--line);background:#080c12;border-radius:8px;padding:12px;color:var(--muted);font-size:13px;line-height:1.45}
.empty{color:var(--muted);font-size:13px;padding:10px;border:1px dashed var(--line);border-radius:8px}
@media(max-width:1180px){.grid{grid-template-columns:240px 1fr}.side{grid-column:1/-1}.stats{grid-template-columns:repeat(2,1fr)}}@media(max-width:760px){.shell{padding:10px}.top,.grid{grid-template-columns:1fr}.indicators{justify-content:flex-start}.event{grid-template-columns:1fr}.stats{grid-template-columns:1fr 1fr}}
</style>
</head>
<body>
<div class="shell">
  <header class="top">
    <section class="brand"><h1>legwork serve</h1><div class="sub">Live operator console over the local state dir. Read-only; mutations stay in the CLI.</div></section>
    <div class="indicators"><span class="pill good">read-only</span><span class="pill good">local</span><span id="live" class="pill warn">connecting</span><span id="stamp" class="pill">no snapshot</span></div>
  </header>
  <main class="grid">
    <aside class="panel"><div class="head"><div class="title">Runs</div><span id="runCount" class="pill">0</span></div><div class="body"><div id="runs" class="runs"></div></div></aside>
    <section class="panel"><div class="head"><div><div id="focusTitle" class="title">All runs</div><div id="focusSub" class="sub"></div></div></div><div class="body">
      <div class="stats"><div class="stat"><div id="jobsNum" class="num">0</div><div class="label">jobs</div></div><div class="stat"><div id="activeNum" class="num">0</div><div class="label">active/queued</div></div><div class="stat"><div id="attnNum" class="num">0</div><div class="label">attention</div></div><div class="stat"><div id="costNum" class="num">$0.00</div><div class="label">known spend</div></div></div>
      <div id="attention" class="attention"></div>
      <table><thead><tr><th>Job</th><th>State</th><th>Task</th><th>Ctx</th><th>Updated</th></tr></thead><tbody id="jobs"></tbody></table>
      <h3 class="title" style="margin:16px 0 8px">Significant timeline</h3><div id="timeline" class="timeline"></div>
    </div></section>
    <aside class="panel side"><div class="head"><div class="title">Selected job</div></div><div id="detail" class="body detail"></div></aside>
  </main>
</div>
<script>
let snapshot=null, selectedRun=null, selectedJob=null;
const $=id=>document.getElementById(id);
function esc(s){return String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function ago(ts){if(!ts)return '-';const s=Math.max(0,(Date.now()-new Date(ts))/1000);if(s<60)return Math.floor(s)+'s';if(s<3600)return Math.floor(s/60)+'m';if(s<86400)return Math.floor(s/3600)+'h';return Math.floor(s/86400)+'d'}
function ctx(n){if(!n)return '-';return n<1000?String(n):Math.floor(n/1000)+'k'}
function money(n){return '$'+Number(n||0).toFixed(2)}
function stateCount(map,names){return names.reduce((a,n)=>a+(map?.[n]||0),0)}
async function load(){const q=selectedJob?'?job='+encodeURIComponent(selectedJob):'';const r=await fetch('/api/snapshot'+q,{cache:'no-store'});snapshot=await r.json();if(!selectedJob&&snapshot.selected)selectedJob=snapshot.selected.id;render()}
function render(){if(!snapshot)return;$('stamp').textContent='updated '+ago(snapshot.generated_at);$('runCount').textContent=snapshot.runs.length;renderRuns();const jobs=snapshot.jobs.filter(j=>selectedRun===null||j.run===selectedRun);$('focusTitle').textContent=selectedRun===null?'All runs':(selectedRun||'(no run)');$('focusSub').textContent=snapshot.state_dir;const active=jobs.filter(j=>j.state==='active'||j.state==='queued').length;const attn=snapshot.attention.filter(a=>selectedRun===null||a.run===selectedRun).length;const cost=jobs.reduce((a,j)=>a+(j.cost_usd||0),0);$('jobsNum').textContent=jobs.length;$('activeNum').textContent=active;$('attnNum').textContent=attn;$('costNum').textContent=money(cost);renderAttention();renderJobs(jobs);renderTimeline();renderDetail()}
function renderRuns(){const rows=[{label:null,display:'All runs',state:'combined',jobs:{},cost_usd:snapshot.jobs.reduce((a,j)=>a+(j.cost_usd||0),0)}].concat(snapshot.runs);$('runs').innerHTML=rows.map(r=>'<button class="run '+(selectedRun===r.label?'active':'')+'" data-run="'+(r.label===null?'__all__':esc(r.label))+'"><div class="run-name">'+esc(r.display||r.label||'(no run)')+'</div><div class="run-meta">'+esc(r.state||'')+' · '+money(r.cost_usd)+'</div>'+(r.last_note?'<div class="note">'+esc(r.last_note)+'</div>':'')+'</button>').join('')||'<div class="empty">No runs yet.</div>';document.querySelectorAll('.run').forEach(b=>b.onclick=()=>{selectedRun=b.dataset.run==='__all__'?null:b.dataset.run;render()})}
function renderAttention(){const items=snapshot.attention.filter(a=>selectedRun===null||a.run===selectedRun).slice(0,5);$('attention').innerHTML=items.map(a=>'<div class="attn '+esc(a.severity)+'"><strong>'+esc(a.job_id)+' · '+esc(a.state)+'</strong><p>'+esc(a.message)+'</p></div>').join('')||(snapshot.jobs.length?'<div class="empty">No attention items.</div>':'')}
function renderJobs(jobs){$('jobs').innerHTML=jobs.map(j=>'<tr class="'+(selectedJob===j.id?'selected':'')+'" data-job="'+esc(j.id)+'"><td><span class="jobid">'+esc(j.id)+'</span><div class="run-meta">'+esc(j.run||'(no run)')+'</div></td><td><span class="state '+esc(j.state)+'">'+esc(j.state)+'</span></td><td><div class="task">'+esc(j.task)+'</div></td><td class="'+(j.context_high?'ctxhigh':'')+'">'+ctx(j.context)+'</td><td>'+ago(j.updated)+'</td></tr>').join('')||'<tr><td colspan="5" class="muted">No jobs.</td></tr>';document.querySelectorAll('tr[data-job]').forEach(r=>r.onclick=()=>{selectedJob=r.dataset.job;load()})}
function renderTimeline(){const items=snapshot.timeline.filter(e=>selectedRun===null||e.run===selectedRun).slice(-40).reverse();$('timeline').innerHTML=items.map(e=>'<div class="event"><div class="time">'+ago(e.ts)+'</div><div><div class="etype">'+esc(e.type)+'</div><div class="src">'+esc(e.job_id||('['+(e.run||'run')+']'))+'</div></div><div class="preview">'+esc(e.preview||'')+'</div></div>').join('')||'<div class="empty">No significant events yet.</div>'}
function renderDetail(){const d=snapshot.selected;if(!d){$('detail').innerHTML='<div class="empty">No job selected.</div>';return}$('detail').innerHTML='<h2>'+esc(d.id)+'</h2><div class="kv"><div>state</div><div><span class="state '+esc(d.state)+'">'+esc(d.state)+'</span></div><div>run</div><div>'+esc(d.run||'(no run)')+'</div><div>agent</div><div>'+esc(d.agent)+(d.model?' / '+esc(d.model):'')+'</div><div>workspace</div><div>'+esc(d.workspace||'-')+'</div><div>cost</div><div>'+money(d.cost_usd)+' · ctx '+ctx(d.context)+'</div><div>task</div><div>'+esc(d.task)+'</div></div>'+(d.question?'<div class="question"><strong>needs input</strong><br>'+esc(d.question)+'</div>':'')+'<div class="disabled"><button disabled>answer in CLI</button><button disabled>open diff in CLI</button></div><div class="placeholder">Diff/review is intentionally a placeholder in v1. This browser surface is observational; review and mutation commands remain explicit CLI actions.</div><h3 class="title" style="margin:16px 0 8px">Recent job events</h3><div class="timeline">'+(d.events||[]).slice(-20).reverse().map(e=>'<div class="event"><div class="time">'+ago(e.ts)+'</div><div class="etype">'+esc(e.type)+'</div><div class="preview">'+esc(e.preview||'')+'</div></div>').join('')+'</div>'}
load().catch(e=>{$('live').textContent='snapshot error';console.error(e)});
try{const es=new EventSource('/events');es.addEventListener('open',()=>{$('live').textContent='live';$('live').className='pill live'});es.addEventListener('snapshot',()=>load());es.addEventListener('heartbeat',()=>{$('live').textContent='live';$('live').className='pill live'});es.onerror=()=>{$('live').textContent='reconnecting';$('live').className='pill warn'}}catch(e){setInterval(load,1500)}
</script>
</body>
</html>`
