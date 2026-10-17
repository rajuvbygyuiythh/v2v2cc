const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {JSDOM}=require('jsdom');
const base=path.join(__dirname,'../cmd/chitchatui/frontend/dist');
const general=()=>({first_message:{mode:'user',bot_first_percent:70,wait_timeout:false,wait_min_seconds:30,wait_max_seconds:60},reply_delay_min_seconds:3,reply_delay_max_seconds:6,match_reply_timeout_min_seconds:30,match_reply_timeout_max_seconds:60,match_close_wait_min_seconds:2,match_close_wait_max_seconds:4,poll_min_seconds:1,poll_max_seconds:2,action_min_seconds:1,action_max_seconds:3});
const show=()=>({first_message:{mode:'random',bot_first_percent:80,wait_timeout:true,wait_min_seconds:10,wait_max_seconds:20},reply_delay_min_seconds:7,reply_delay_max_seconds:9,no_reply_min_seconds:45,no_reply_max_seconds:50,next_chat_min_seconds:1,next_chat_max_seconds:3,auto_skip:true});
const readyRow=()=>({id:'show-1',profile:'Profile 1',account:'synthetic',status:'ready',detail:'Logged in',decision:'',message_mode:''});
async function setup(unified=true,failLoad=false) {
 const dom=new JSDOM(fs.readFileSync(path.join(base,'index.html'),'utf8'),{runScripts:'outside-only'}),w=dom.window,d=w.document;w.addEventListener=()=>{};
 const calls=[],settings={general:general(),show_auto_skip:true,unified,legacy_show:show()},control={revision:7,settings:show(),active:false,browsers:[readyRow()]};
 w.go={main:{App:{GetChatSettings:async()=>{if(failLoad)throw new Error("load failed");return JSON.parse(JSON.stringify(settings));},SaveChatSettings:async x=>{calls.push(['save',x]);Object.assign(settings,JSON.parse(JSON.stringify(x)),{unified:true});control.revision++;},GetShowControlState:async()=>JSON.parse(JSON.stringify(control)),StartShowChat:async(...args)=>{calls.push(['start',...args]);control.active=true;control.browsers[0]={...readyRow(),status:'running',settings:show()};},StopShowChat:async id=>{calls.push(['stop',id]);control.active=false;control.browsers[0]=readyRow();},RunBrowser:async()=>calls.push(['open'])}}};
 w.eval(fs.readFileSync(path.join(base,'app.js'),'utf8'));w.bindForms();w.bindWorkspace();await w.refreshSharedSettings();await w.refreshShowControl();
 return {dom,w,d,calls,settings,control};
}
const tick=()=>new Promise(r=>setImmediate(r));
const change=(w,d,id,value)=>{d.getElementById(id).value=value;d.getElementById(id).dispatchEvent(new w.Event('change',{bubbles:true}));};
test('one common form; Home has controls and Browser has login only',async()=>{
 const {dom,w,d}=await setup();assert.equal(d.querySelectorAll('#general-form').length,1);assert.equal(d.querySelector('#show-mode-form'),null);assert.equal(d.querySelector('#show-first-mode'),null);
 assert.equal(d.querySelector('#general-form').closest('[data-panel]').dataset.panel,'setting');assert.equal(d.querySelector('#settings-show').closest('[data-panel]').dataset.panel,'home');
 assert.equal(d.querySelector('#diagnostic-details').open,false);assert.equal(d.querySelector('.advanced-settings').open,false);assert.equal(d.querySelector('.advanced-settings').hidden,true);
 assert.equal(d.getElementById('settings-show').hidden,false);change(w,d,'workspace-mode','standard');assert.equal(d.getElementById('settings-show').hidden,true);assert.equal(d.getElementById('standard-actions').hidden,false);assert.equal(d.querySelector('.advanced-settings').hidden,false);
 d.querySelector('[data-open-tab="setting"]').click();assert.equal(d.querySelector('[data-panel="setting"]').classList.contains('active'),true);dom.window.close();
});
test('one Save sends three shared timings, preserves Standard policy, and enables Show skip',async()=>{
 const {dom,w,d,calls}=await setup();await w.saveSharedSettings();assert.equal(calls.length,1);const payload=calls[0][1];assert.equal(calls[0][0],'save');assert.equal(payload.general.reply_delay_min_seconds,3);assert.equal(payload.general.first_message.mode,'user');assert.equal(payload.general.poll_min_seconds,1);assert.equal('show_auto_skip' in payload,false);assert.match(d.getElementById('standard-save-status').textContent,/both modes/);dom.window.close();
});
test('first setup requires explicit profile choice and preserves mode-specific values',async()=>{
 const {dom,w,d,calls}=await setup(false);assert.equal(d.getElementById('settings-migration').hidden,false);await w.saveSharedSettings();assert.equal(calls.length,0);assert.match(d.getElementById('standard-save-status').textContent,/Choose/);
 change(w,d,'settings-source','show');assert.equal(d.getElementById('worker-reply-delay-min').value,'7');assert.equal(d.getElementById('standard-first-mode'),null);assert.equal(d.getElementById('poll-min').value,'1');
 change(w,d,'settings-source','standard');assert.equal(d.getElementById('worker-reply-delay-min').value,'3');assert.equal(d.getElementById('show-auto-skip'),null);await w.saveSharedSettings();assert.equal(calls.length,1);assert.equal(d.getElementById('settings-migration').hidden,true);dom.window.close();
});
test('invalid reply/idle ranges and zero silence timeout cannot reach backend',async()=>{
 const {dom,w,d,calls}=await setup();
 for(const [id,value,pattern] of [['worker-reply-delay-min',99,/min/],['match-reply-timeout-min',0,/at least 1/],['match-close-wait-min',5,/min/]]) {
  w.fillSharedValues(general(),true);d.getElementById(id).value=value;await w.saveSharedSettings();assert.equal(calls.length,0);assert.match(d.getElementById('standard-save-status').textContent,pattern);
 }dom.window.close();
});
test('Show has exactly three timing groups and no first-turn or skip switch controls',async()=>{
 const {dom,d}=await setup();
 assert.deepEqual([...d.querySelectorAll('#general-form > .shared-timing-grid legend')].map(n=>n.textContent),['Reply delay','Next chat wait','User silent timeout']);
 assert.equal(d.querySelectorAll('.first-message-settings').length,0);assert.equal(d.querySelector('[id*="first-mode"], [id*="first-wait"], #show-auto-skip'),null);
 assert.match(d.querySelector('#general-form').textContent,/same silent timeout before the first user message/);dom.window.close();
});
test('dirty common settings block both Starts and Show browser launch; polling preserves draft',async()=>{
 const {dom,w,d,calls}=await setup();change(w,d,'worker-reply-delay-min',9);assert.equal(d.getElementById('start-bot').disabled,true);assert.equal(d.querySelector('#show-sessions .primary-button').disabled,true);
 await w.controlShowChat('show-1',true);await w.runBrowserOption({key:'show_mode',label:'Show Mode'});assert.equal(calls.length,0);await w.refreshShowControl();await w.refreshSharedSettings();assert.equal(d.getElementById('worker-reply-delay-min').value,'9');dom.window.close();
});
test('either running mode locks Save; Show Start sends revision and keeps real frozen summary',async()=>{
 const {dom,w,d,calls}=await setup();await w.controlShowChat('show-1',true);assert.deepEqual(calls[0],['start','show-1',7]);assert.equal(d.getElementById('worker-reply-delay-min').disabled,true);await w.saveSharedSettings();assert.equal(calls.length,1);assert.match(d.querySelector('#show-sessions').textContent,/Reply delay: 7–9s/);
 await w.controlShowChat('show-1',false);assert.equal(d.getElementById('worker-reply-delay-min').disabled,false);w.setRunning(true);assert.equal(d.getElementById('save-chat-settings').disabled,true);await w.saveSharedSettings();assert.equal(calls.length,2);w.setRunning(false);assert.equal(d.getElementById('save-chat-settings').disabled,false);dom.window.close();
});
test('failed Save retains dirty draft and displays error; successful retry works',async()=>{
 const {dom,w,d,calls}=await setup();const api=w.go.main.App,original=api.SaveChatSettings;api.SaveChatSettings=async()=>{throw new Error('synthetic save failure')};change(w,d,'worker-reply-delay-min',4);await w.saveSharedSettings();assert.match(d.getElementById('standard-save-status').textContent,/synthetic save failure/);assert.equal(d.getElementById('worker-reply-delay-min').value,'4');assert.equal(d.getElementById('start-bot').disabled,true);api.SaveChatSettings=original;await w.saveSharedSettings();assert.equal(calls.length,1);assert.equal(d.getElementById('start-bot').disabled,false);dom.window.close();
});
test('busy account and untrusted labels remain safe',async()=>{
 const {dom,w,d,control}=await setup();control.browsers[0]={...readyRow(),profile:'<img src=x onerror=alert(1)>',blocked_by:'standard:1:test'};await w.refreshShowControl();assert.equal(d.querySelector('#show-sessions .primary-button').disabled,true);assert.equal(d.querySelector('#show-sessions img'),null);assert.match(d.querySelector('#show-sessions').textContent,/standard:1:test/);dom.window.close();
});
test('late common response or concurrent draft edit cannot overwrite current form',async()=>{
 const {dom,w,d}=await setup();const pending=[];w.go.main.App.GetChatSettings=()=>new Promise(resolve=>pending.push(resolve));const first=w.refreshSharedSettings(),second=w.refreshSharedSettings();
 pending[1]({general:{...general(),reply_delay_min_seconds:5},show_auto_skip:true,unified:true});await second;pending[0]({general:{...general(),reply_delay_min_seconds:1},show_auto_skip:true,unified:true});await first;assert.equal(d.getElementById('worker-reply-delay-min').value,'5');
 const third=w.refreshSharedSettings();change(w,d,'worker-reply-delay-min',6);pending[2]({general:general(),unified:true});await third;assert.equal(d.getElementById('worker-reply-delay-min').value,'6');dom.window.close();
});
test('late Show status response cannot overwrite newer status or revision',async()=>{
 const {dom,w,d,calls}=await setup(),pending=[];w.go.main.App.GetShowControlState=()=>new Promise(resolve=>pending.push(resolve));const first=w.refreshShowControl(),second=w.refreshShowControl();pending[1]({revision:9,active:false,browsers:[readyRow()]});await second;pending[0]({revision:8,active:true,browsers:[]});await first;assert.equal(d.querySelector('#show-sessions .primary-button').disabled,false);assert.equal(d.getElementById('worker-reply-delay-min').disabled,false);dom.window.close();
});
test('Show profile with disabled zero timeout retains a positive Standard timeout for sharing',async()=>{
 const {dom,w,d,settings}=await setup(false);settings.legacy_show.no_reply_min_seconds=0;settings.legacy_show.no_reply_max_seconds=0;settings.show_auto_skip=false;
 await w.refreshSharedSettings();change(w,d,'settings-source','show');assert.equal(d.getElementById('match-reply-timeout-min').value,'30');assert.equal(d.getElementById('worker-reply-delay-min').value,'7');dom.window.close();
});
test('switching workspace does not start/stop anything or show Show transcript as Standard',async()=>{
 const {dom,w,d,calls}=await setup();change(w,d,'workspace-mode','standard');assert.equal(d.getElementById('live-chat-workspace').hidden,true);assert.equal(d.getElementById('standard-mode-note').hidden,false);change(w,d,'workspace-mode','show');assert.equal(d.getElementById('live-chat-workspace').hidden,false);assert.equal(calls.length,0);dom.window.close();
});
test('loading failure keeps Start/Save disabled; refresh can recover',async()=>{
 const {dom,w,d,settings}=await setup(true,true);assert.equal(d.getElementById('save-chat-settings').disabled,true);assert.equal(d.getElementById('start-bot').disabled,true);w.go.main.App.GetChatSettings=async()=>settings;await w.refreshSharedSettings();assert.equal(d.getElementById('save-chat-settings').disabled,false);assert.equal(d.querySelector('#show-sessions .primary-button').disabled,false);dom.window.close();
});

test('Standard restores its first policy in the same contextual form; Show saves only shared values',async()=>{
 const {dom,w,d,calls}=await setup();
 assert.equal(d.querySelectorAll('#standard-policy').length,1);assert.equal(d.querySelector('#standard-policy').closest('details').hidden,true);
 change(w,d,'workspace-mode','standard');assert.equal(d.getElementById('standard-policy-mode').value,'user');assert.equal(d.getElementById('standard-policy-min').disabled,true);
 change(w,d,'standard-policy-mode','random');change(w,d,'standard-policy-chance',35);d.getElementById('standard-policy-timeout').checked=true;d.getElementById('standard-policy-timeout').dispatchEvent(new w.Event('change',{bubbles:true}));
 change(w,d,'standard-policy-min',8);change(w,d,'standard-policy-max',11);change(w,d,'poll-min',2);
 await w.saveSharedSettings();assert.equal(calls[0][1].edit_mode,'standard');assert.deepEqual(JSON.parse(JSON.stringify(calls[0][1].general.first_message)),{mode:'random',bot_first_percent:35,wait_timeout:true,wait_min_seconds:8,wait_max_seconds:11});assert.equal(calls[0][1].general.poll_min_seconds,2);
 change(w,d,'workspace-mode','show');change(w,d,'poll-min',-999);change(w,d,'standard-policy-chance',999);await w.saveSharedSettings();
 assert.equal(calls[1][1].edit_mode,'show');assert.equal(calls[1][1].general.poll_min_seconds,2);assert.equal(calls[1][1].general.first_message.bot_first_percent,35);assert.equal(d.querySelector('#standard-policy').closest('details').hidden,true);dom.window.close();
});
test('invalid Standard first policy cannot save and running locks cannot be undone by policy changes',async()=>{
 const {dom,w,d,calls}=await setup();change(w,d,'workspace-mode','standard');change(w,d,'standard-policy-mode','random');change(w,d,'standard-policy-chance',101);
 await w.saveSharedSettings();assert.equal(calls.length,0);assert.match(d.getElementById('standard-save-status').textContent,/0 to 100/);
 w.setRunning(true);change(w,d,'standard-policy-mode','random');assert.equal(d.getElementById('standard-policy-chance').disabled,true);assert.equal(d.getElementById('standard-policy-timeout').disabled,true);
 w.setRunning(false);change(w,d,'standard-policy-chance',25);d.getElementById('standard-policy-timeout').checked=true;change(w,d,'standard-policy-min',0);
 await w.saveSharedSettings();assert.equal(calls.length,0);assert.match(d.getElementById('standard-save-status').textContent,/positive minimum/);dom.window.close();
});
test('read-only mode debug ignores late responses and safely renders backend labels',async()=>{
 const {dom,w,d}=await setup();const pending=[];
 w.go.main.App.GetExecutionSettings=mode=>new Promise(resolve=>pending.push({mode,resolve}));
 const old=w.refreshExecutionPreview();change(w,d,'workspace-mode','standard');
 assert.deepEqual(pending.map(x=>x.mode),['show','standard']);pending[1].resolve({mode:'standard',driver:'<img src=x onerror=alert(1)>',source:'canonical_v2',revision:3,purpose:'Standard next Start preview',standard:general()});await tick();
 const wanted=d.getElementById('execution-settings').textContent;assert.match(wanted,/standard/);assert.match(wanted,/Poll 1–2s/);assert.equal(d.querySelector('#execution-settings img'),null);
 pending[0].resolve({mode:'show',driver:'rod',source:'old'});await old;assert.equal(d.getElementById('execution-settings').textContent,wanted);assert.equal(d.querySelector('#execution-debug input'),null);dom.window.close();
});
test('mode switching shares timing drafts but cannot silently discard Standard-only edits',async()=>{
 const {dom,w,d,calls}=await setup();change(w,d,'worker-reply-delay-min',4);change(w,d,'workspace-mode','standard');assert.equal(d.getElementById('workspace-mode').value,'standard');assert.equal(d.getElementById('worker-reply-delay-min').value,'4');
 change(w,d,'standard-policy-mode','bot');change(w,d,'workspace-mode','show');assert.equal(d.getElementById('workspace-mode').value,'standard');assert.match(d.getElementById('standard-save-status').textContent,/Save Standard-only/);
 await w.saveSharedSettings();assert.equal(calls[0][1].edit_mode,'standard');change(w,d,'workspace-mode','show');assert.equal(d.getElementById('workspace-mode').value,'show');assert.equal(d.querySelector('#standard-policy').closest('details').hidden,true);dom.window.close();
});
