'use strict';
const $ = id => document.getElementById(id);
let state, selected = [], filter = 'all', busy = false, loading = false, rendered = '', toastTimer;
const OFFLINE_FILE_MS = 30000, OFFLINE_TEXT_MS = 600000;
let preferences = {devices:{}, items:{}, hues:{}};
function readPreferences() { try { preferences = {...preferences, ...JSON.parse(localStorage.getItem('visibility-' + state.self.id) || '{}')}; } catch {} if(preferences.paletteVersion!==2){preferences.hues={};preferences.paletteVersion=2;} }
function persistPreferences() { localStorage.setItem('visibility-' + state.self.id, JSON.stringify(preferences)); rendered = ''; render(); }
function offlineAge(peer) { return peer.online ? 0 : Math.max(0, Date.now() - new Date(peer.offlineSince).getTime()); }
function visibleItem(peer,item) { return !preferences.devices[peer.id] && !preferences.items[peer.id + '/' + item.id] && (peer.online || offlineAge(peer) < (item.kind === 'text' ? OFFLINE_TEXT_MS : OFFLINE_FILE_MS)); }
function tint(el,id) {
 if (preferences.hues[id] === undefined) {
  const used = Object.values(preferences.hues);
  let hue = 0, best = -1;
  // Pick a random cool/rose hue, favoring distance from existing device colors.
  for(let i=0;i<80;i++) {
   const candidate=160+Math.random()*185;
   const distance=used.length?Math.min(...used.map(v=>Math.min(Math.abs(candidate-v),360-Math.abs(candidate-v)))):1;
   if(distance>best){best=distance;hue=candidate;}
  }
  preferences.hues[id] = hue; localStorage.setItem('visibility-' + state.self.id, JSON.stringify(preferences));
 }
 const hue=preferences.hues[id]; el.style.setProperty('--tint',`hsl(${hue} 68% 79%)`); el.style.setProperty('--wash',`hsl(${hue} 48% 43%)`); el.dataset.device=id;
}
async function changeSettings(changes) { await api('/api/settings',{...state.self,...changes}); await load(); }
function manager() {
 $('deviceVisibility').replaceChildren();
 for (const p of [{id:'self',name:'我的共享',items:state.self.items},...state.peers]) {
  const group=element('section','device-group'); tint(group,p.id==='self'?state.self.id:p.id);
  const title=element('div','manager-device'); title.append(element('strong','',p.name));
  if(p.id!=='self') { const toggle=element('button','chip',preferences.devices[p.id]?'显示设备':'隐藏设备');toggle.onclick=()=>{preferences.devices[p.id]=!preferences.devices[p.id];persistPreferences();manager();};title.append(toggle); }
  group.append(title);
  for(const item of p.items) {
   const row=element('label','switch-line'); const label=element('span','',item.kind==='text'?item.text:item.name); const check=element('input');check.type='checkbox';check.checked=p.id==='self'?!item.hidden:!preferences.items[p.id+'/'+item.id];check.setAttribute('aria-label',p.id==='self'?'共享 '+label.textContent:'显示 '+label.textContent);
   check.onchange=async()=>{try{if(p.id==='self'){await api(`/api/items/${item.id}/visibility`,{hidden:!check.checked});await load();}else{preferences.items[p.id+'/'+item.id]=!check.checked;persistPreferences();}}catch(e){check.checked=!check.checked;toast(e.message);}};row.append(label,check);group.append(row);
  }
  if(!p.items.length)group.append(element('p','muted','暂无内容')); $('deviceVisibility').append(group);
 }
}
const size = n => n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : n < 1073741824 ? `${(n / 1048576).toFixed(1)} MB` : `${(n / 1073741824).toFixed(2)} GB`;
function element(tag, cls, text) { const el = document.createElement(tag); if (cls) el.className = cls; if (text !== undefined) el.textContent = text; return el; }
function toast(message) { $('toast').textContent = message; $('toast').hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => $('toast').hidden = true, 4200); }
async function api(path, body, method = 'POST') { const res = await fetch(path, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) }); if (!res.ok) throw Error(await res.text()); return res.json(); }
async function load() {
 if (loading) return; loading = true;
 try { const res = await fetch('/api/state'); if (!res.ok) throw Error(); state = await res.json(); readPreferences(); $('toggleSharing').textContent=state.self.enabled?'共享：开':'共享：关'; $('toggleSharing').setAttribute('aria-pressed',String(state.self.enabled)); $('toggleAutoSave').textContent=state.self.autoSave?'自动保存：开':'自动保存：关'; $('toggleAutoSave').setAttribute('aria-pressed',String(!!state.self.autoSave)); $('deviceSettings').classList.toggle('paused',!state.self.enabled); $('deviceName').textContent = state.self.name; $('syncStatus').textContent = state.self.enabled ? '自动更新中 · 设备直接连接' : '本机共享已暂停'; if(state.saveError) $('syncStatus').textContent=state.saveError;
  const view = JSON.stringify({ self: state.self, peers: state.peers.map(({ updated, ...p }) => p).sort((a,b) => a.id.localeCompare(b.id)), filter, expiry: state.peers.map(p=>[p.id,offlineAge(p)<OFFLINE_FILE_MS,offlineAge(p)<OFFLINE_TEXT_MS]).sort((a,b)=>a[0].localeCompare(b[0])) });
  if (view !== rendered) { rendered = view; render(); }
 } catch { $('syncStatus').textContent = '正在重新连接 · 已有内容保留'; } finally { loading = false; }
}
function contentURL(peer, item, download = false) { return `/api/content/${encodeURIComponent(peer)}/${encodeURIComponent(item.id)}${download ? '?download=1' : ''}`; }
function render() {
 const peers = state.peers.sort((a,b) => a.name.localeCompare(b.name));
 $('peerCount').textContent = `${peers.filter(p => p.online).length} 台附近设备`;
 $('filters').querySelectorAll('.device-filter').forEach(e => e.remove());
 if (filter !== 'all' && filter !== 'self' && !peers.some(p=>p.id===filter && !preferences.devices[p.id] && (p.online || offlineAge(p)<OFFLINE_FILE_MS))) filter='all';
 for (const p of peers.filter(p=>!preferences.devices[p.id] && (p.online || offlineAge(p)<OFFLINE_FILE_MS))) { const button = element('button', 'chip device-filter' + (filter === p.id ? ' selected' : ''), p.name + (p.online ? '' : ' · 离线')); tint(button,p.id); button.onclick = () => setFilter(p.id); $('filters').append(button); }
 $('filters').querySelectorAll('[data-filter]').forEach(e => e.classList.toggle('selected', e.dataset.filter === filter));
 $('peerStatus').replaceChildren();
 for (const p of peers) { if (preferences.devices[p.id] || (!p.online && offlineAge(p)>=OFFLINE_FILE_MS) || !p.error || (filter !== 'all' && filter !== p.id)) continue; const note = element('div','peer-note'); note.append(element('span','',`${p.name} · ${p.error}`));
  if (p.locked) { const form = element('form'); const input = element('input'); input.type = 'password'; input.placeholder = '输入共享码'; input.setAttribute('aria-label', `${p.name} 的共享码`); const button = element('button','light-button','解锁'); form.append(input, button); form.onsubmit = async e => { e.preventDefault(); try { await api(`/api/unlock/${p.id}`, {code:input.value}); await load(); if (state.peers.find(x => x.id === p.id)?.locked) toast('共享码不正确'); } catch(e) { toast(e.message); } }; note.append(form); }
  $('peerStatus').append(note);
 }
 const entries = [];
 if (filter === 'all' || filter === 'self') for (const item of state.self.items) entries.push({item,peer:'self',name:'我的共享',online:true});
 for (const p of peers) if (filter === 'all' || filter === p.id) for (const item of p.items.filter(item=>visibleItem(p,item))) entries.push({item,peer:p.id,name:p.name,online:p.online});
 entries.sort((a,b) => new Date(b.item.created) - new Date(a.item.created)); $('itemCount').textContent = entries.length; $('empty').hidden = entries.length > 0; $('items').replaceChildren();
 for (const {item,peer,name,online} of entries) {
  const card = element('article','item'); card.dataset.kind = item.kind; tint(card,peer==='self'?state.self.id:peer); const top = element('div','item-top'); top.append(element('span','item-source',name)); if (item.hidden) top.append(element('span','offline-badge','未共享')); if (!online) top.append(element('span','offline-badge','离线')); card.append(top);
  if (item.kind === 'text') card.append(element('div','item-text',item.text));
  else {
   const title = element('h3','file-title',item.name); title.title = item.name; card.append(title);
   const file = element('div','file-summary'); card.append(file);
   if (!online) file.append(element('span','file-badge','设备离线'));
   else if (item.kind === 'image') {
    const preview = element('button','preview'); preview.setAttribute('aria-label',`查看 ${item.name}`); preview.setAttribute('aria-expanded','false');
    const img = element('img'); img.alt = item.name; img.loading = 'lazy'; img.src = contentURL(peer,item); img.onerror = () => { img.replaceWith(element('span','file-icon','▧')); }; preview.append(img);
    preview.onclick = () => { const expanded = card.classList.toggle('expanded'); preview.setAttribute('aria-expanded',String(expanded)); }; file.append(preview);
   } else if (item.kind === 'video') {
    const play = element('button','file-icon play','▶'); play.setAttribute('aria-label',`播放 ${item.name}`); play.setAttribute('aria-expanded','false');
    const video = element('video'); video.src = contentURL(peer,item); video.controls = true; video.preload = 'none'; video.hidden = true; video.setAttribute('playsinline','');
    play.onclick = async () => { video.hidden = !video.hidden; play.setAttribute('aria-expanded',String(!video.hidden)); if(video.hidden) video.pause(); else try { await video.play(); } catch { toast('暂时无法播放，可保存后打开'); } }; file.append(play); card.append(video);
   } else file.append(element('span','file-badge',item.name.split('.').pop().slice(0,8).toUpperCase()));
   file.append(element('span','meta',size(item.size)));
  }
  const actions = element('div','item-actions');
  if (item.kind === 'text') { const copy = element('button','','复制'); copy.setAttribute('aria-label','复制文字'); copy.onclick = async () => { try { if (window.Android) Android.copyText(item.text); else await navigator.clipboard.writeText(item.text); toast('已复制'); } catch { toast('复制失败，请选中文字复制'); } }; actions.append(copy); }
  if (online || item.kind === 'text') { const save = element('a','','保存'); save.href = contentURL(peer,item,true); save.download = item.kind === 'text' ? '文字片段.txt' : item.name;
  save.onclick = e => { if (window.Android) {e.preventDefault();Android.download(new URL(save.href).href,save.download);toast('开始保存，可在系统通知中查看');} }; actions.append(save); }
  if (peer === 'self') { const remove = element('button','remove','×'); remove.title = '撤下共享'; remove.setAttribute('aria-label',`撤下 ${item.kind === 'text' ? '文字' : item.name}`); remove.onclick = async () => { try { await api(`/api/items/${item.id}`,undefined,'DELETE'); await load(); toast('已撤下共享，原始文件保留'); } catch(e) {toast(e.message);} }; top.append(remove); }
  if(peer==='self') { const visibility=element('button','',item.hidden?'恢复共享':'暂停此条'); visibility.onclick=async()=>{try{await api(`/api/items/${item.id}/visibility`,{hidden:!item.hidden});await load();}catch(e){toast(e.message);}};actions.append(visibility); }
  else {const hide=element('button','','隐藏');hide.setAttribute('aria-label',`隐藏 ${item.kind==='text'?item.text:item.name}`);hide.onclick=()=>{preferences.items[peer+'/'+item.id]=true;persistPreferences();};actions.append(hide);}
  card.append(actions); $('items').append(card);
 }
}
function setFilter(value) { filter = value; rendered = ''; if (state) render(); }
function updateDraft() { $('publish').disabled = busy || (!selected.length && !$('text').value.trim()); $('textCount').textContent = $('text').value.length ? `${$('text').value.length} 字` : ''; }
function pick(files) { for (const file of files) selected.push({file,preview:file.type.startsWith('image/') ? URL.createObjectURL(file) : null}); renderPending(); }
function renderPending() {
 $('pending').replaceChildren(); $('pending').hidden = !selected.length;
 selected.forEach((entry,i) => { const chip = element('div','pending-item'); if(entry.preview) { const img = element('img');img.src=entry.preview;img.alt='';chip.append(img); } chip.append(element('span','',entry.file.name)); const remove = element('button','','×'); remove.setAttribute('aria-label',`移除 ${entry.file.name}`);remove.disabled=busy;remove.onclick=()=>{if(entry.preview)URL.revokeObjectURL(entry.preview);selected.splice(i,1);renderPending();};chip.append(remove);$('pending').append(chip); }); updateDraft();
}
function upload(files) { return new Promise((resolve,reject) => { const xhr = new XMLHttpRequest(); xhr.open('POST','/api/files'); const data = new FormData(); files.forEach(f => data.append('files',f.file,f.file.name)); xhr.upload.onprogress = e => { if(e.lengthComputable) {$('progress').value=e.loaded/e.total*100;$('composeHint').textContent=`正在添加 ${Math.round(e.loaded/e.total*100)}%`;}}; xhr.onerror=()=>reject(Error('连接中断，已完成的文件会保留'));xhr.onload=()=>xhr.status===200 ? resolve() : reject(Error(xhr.responseText || '文件添加失败'));xhr.send(data); }); }
$('publish').onclick = async () => { if(busy)return;busy=true;updateDraft();$('pickFiles').disabled=true;$('pickPhotos').disabled=true;$('text').disabled=true;$('progress').hidden=false;renderPending();try { if(selected.length) {await upload(selected);selected.forEach(f=>{if(f.preview)URL.revokeObjectURL(f.preview);});selected=[];renderPending();} if($('text').value.trim()) {await api('/api/text',{text:$('text').value});$('text').value='';} toast('已发布，附近设备将自动看到'); } catch(e) {toast(e.message);} finally {busy=false;$('pickFiles').disabled=false;$('pickPhotos').disabled=false;$('text').disabled=false;$('progress').hidden=true;$('composeHint').textContent='同一网络内可见 · 可拖入文件';renderPending();await load();} };
$('pickFiles').onclick=()=>$('files').click();$('pickPhotos').onclick=()=>$('photos').click();
for(const id of ['files','photos']) $(id).onchange=e=>{pick(e.target.files);e.target.value='';};
$('text').oninput=updateDraft;
for(const name of ['dragenter','dragover']) $('dropzone').addEventListener(name,e=>{e.preventDefault();if(!busy)$('dropzone').classList.add('dragging');});
$('dropzone').ondragleave=()=>$('dropzone').classList.remove('dragging');$('dropzone').ondrop=e=>{e.preventDefault();$('dropzone').classList.remove('dragging');if(!busy)pick(e.dataTransfer.files);};
$('filters').querySelectorAll('[data-filter]').forEach(e=>e.onclick=()=>setFilter(e.dataset.filter));
$('refresh').onclick=async()=>{$('refresh').disabled=true;try{await api('/api/refresh');await load();}catch(e){toast(e.message);}finally{$('refresh').disabled=false;}};
function openSettings(){if(!state)return;$('nameInput').value=state.self.name;$('codeInput').value=state.self.code||'';$('enabledInput').checked=state.self.enabled;$('autoSaveInput').checked=!!state.self.autoSave;$('addresses').textContent=state.addresses.join(' · ');$('settings').showModal();}
$('deviceSettings').onclick=openSettings;$('settingsClose').onclick=()=>$('settings').close();
$('saveSettings').onclick=async()=>{try{await api('/api/settings',{name:$('nameInput').value,enabled:$('enabledInput').checked,code:$('codeInput').value,autoSave:$('autoSaveInput').checked});$('settings').close();await load();toast('设置已保存');}catch(e){toast(e.message);}};
$('connect').onclick=async()=>{$('connect').disabled=true;try{await api('/api/connect',{address:$('addressInput').value,code:$('remoteCode').value});$('settings').close();await load();toast('设备已连接');}catch(e){toast(e.message);}finally{$('connect').disabled=false;}};
$('toggleSharing').onclick=async()=>{try{await changeSettings({enabled:!state.self.enabled});}catch(e){toast(e.message);}};
$('toggleAutoSave').onclick=async()=>{try{await changeSettings({autoSave:!state.self.autoSave});toast(state.self.autoSave?'自动保存已开启 · 下载/邻享':'自动保存已关闭');}catch(e){toast(e.message);}};
$('manageDevices').onclick=()=>{manager();$('contentManager').showModal();};$('managerClose').onclick=()=>$('contentManager').close();
if(window.Android && Android.backgroundSettings){$('backgroundSettings').hidden=false;$('backgroundSettings').onclick=()=>Android.backgroundSettings();}
if(window.Android && Android.appBackgroundSettings){$('vendorBackgroundSettings').hidden=false;$('vendorBackgroundSettings').onclick=()=>Android.appBackgroundSettings();$('backgroundHelp').hidden=false;}
load();setInterval(load,2000);document.addEventListener('visibilitychange',()=>{if(!document.hidden)load();});
