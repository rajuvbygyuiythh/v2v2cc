const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {JSDOM} = require('jsdom');
const base = path.join(__dirname,'../cmd/chitchatui/frontend/dist');
function setup() {
 const dom = new JSDOM(fs.readFileSync(path.join(base,'index.html'),'utf8'),{runScripts:'outside-only'});
 const w = dom.window; w.addEventListener=()=>{};
 const calls=[]; const control={revision:1,settings:{},active:false,browsers:[]}; w.go={main:{App:{GetShowControlState:async()=>control,SaveShowModeSettings:async x=>{calls.push(['show',x]);control.settings=x;control.revision++;},SaveGeneralSettings:async x=>calls.push(['standard',x])}}};
 w.eval(fs.readFileSync(path.join(base,'app.js'),'utf8'));
 w.bindForms();
 const d=w.document;
 for(const [id,value] of Object.entries({'reply-delay-min':3,'reply-delay-max':6,'show-timeout-min':30,'show-timeout-max':60,'show-next-min':2,'show-next-max':4,'worker-reply-delay-min':7,'worker-reply-delay-max':9,'poll-min':1,'poll-max':2,'action-min':1,'action-max':3,'match-reply-timeout-min':20,'match-reply-timeout-max':30,'match-close-wait-min':0,'match-close-wait-max':2})) d.getElementById(id).value=value;
 return {dom,w,d,calls};
}
const tick=()=>new Promise(r=>setImmediate(r));
test('Show settings live in Browser only; Standard settings stay separate',()=>{
 const {dom,w,d}=setup();
 assert.equal(d.getElementById('settings-show').closest('[data-panel]').dataset.panel,'browser');
 assert.equal(d.getElementById('settings-standard').closest('[data-panel]').dataset.panel,'setting');
 assert.equal(d.getElementById('settings-standard').hidden,false);
 assert.equal(d.getElementById('start-bot').textContent,'Start Standard Bot');
 assert.equal(d.getElementById('stop-bot').textContent,'Stop Standard Bot');
 d.getElementById('manage-show-settings').click();
 assert.equal(d.querySelector('[data-panel="browser"]').classList.contains('active'),true);
 assert.equal(d.getElementById('reply-delay-min').value,'3');
 assert.equal(d.querySelectorAll('#reply-delay-min').length,1);
 dom.window.close();
});
test('each Save dispatches only its own timing profile and shows restart notice',async()=>{
 const {dom,w,d,calls}=setup();
 for(const id of ['show-mode-form','general-form']) d.getElementById(id).dispatchEvent(new w.Event('submit',{cancelable:true}));
 await tick();
 assert.deepEqual(calls.map(x=>x[0]),['show','standard']);
 assert.equal(calls[0][1].reply_delay_min_seconds,3);
 assert.equal('poll_min_seconds' in calls[0][1],false);
 assert.equal(calls[1][1].reply_delay_min_seconds,7);
 assert.equal('auto_skip' in calls[1][1],false);
 assert.match(d.getElementById('show-save-status').textContent,/Start Show Chat/);
 assert.match(d.getElementById('standard-save-status').textContent,/restart Start Bot/);
 dom.window.close();
});
test('invalid ranges and nonpositive enabled timeout do not reach backend',async()=>{
 const {dom,w,d,calls}=setup();
 d.getElementById('reply-delay-min').value=10;
 d.getElementById('show-mode-form').dispatchEvent(new w.Event('submit',{cancelable:true}));
 await tick(); assert.equal(calls.length,0);
 d.getElementById('reply-delay-min').value=0;
 d.getElementById('show-auto-skip').checked=true;
 d.getElementById('show-timeout-min').value=0;
 d.getElementById('show-mode-form').dispatchEvent(new w.Event('submit',{cancelable:true}));
 await tick(); assert.equal(calls.length,0);
 assert.match(d.getElementById('show-save-status').textContent,/positive timeout/);
 dom.window.close();
});
test('backend failures are visible within the relevant page',async()=>{
 const {dom,w,d}=setup();
 w.go.main.App.SaveShowModeSettings=async()=>{throw Error('disk unavailable')};
 d.getElementById('show-mode-form').dispatchEvent(new w.Event('submit',{cancelable:true}));
 await tick(); assert.match(d.getElementById('show-save-status').textContent,/disk unavailable/);
 dom.window.close();
});

test('first-message controls reveal only relevant fields and remain independent',()=>{
 const {dom,w,d}=setup();
 const change=(id,value)=>{const e=d.getElementById(id);e.value=value;e.dispatchEvent(new w.Event('change'));};
 assert.equal(d.getElementById('show-first-percent-row').hidden,true);
 assert.equal(d.getElementById('show-first-wait-row').hidden,true);
 change('show-first-mode','random');
 assert.equal(d.getElementById('show-first-percent-row').hidden,false);
 assert.equal(d.getElementById('show-first-wait-row').hidden,false);
 assert.equal(d.getElementById('standard-first-mode').value,'bot');
 change('show-first-wait','skip');assert.equal(d.getElementById('show-first-min').disabled,false);
 change('show-first-mode','user');assert.equal(d.getElementById('show-first-percent-row').hidden,true);
 change('show-first-mode','bot');assert.equal(d.getElementById('show-first-min').disabled,true);
 dom.window.close();
});
test('first-message policies save separately and validate probability',async()=>{
 const {dom,w,d,calls}=setup();
 d.getElementById('show-first-mode').value='random';d.getElementById('show-first-percent').value=80;
 d.getElementById('show-first-wait').value='skip';d.getElementById('standard-first-mode').value='user';
 for(const id of ['show-mode-form','general-form']) d.getElementById(id).dispatchEvent(new w.Event('submit',{cancelable:true}));
 await tick();
 assert.equal(calls[0][1].first_message.mode,'random');assert.equal(calls[0][1].first_message.bot_first_percent,80);
 assert.equal(calls[0][1].first_message.wait_timeout,true);
 assert.equal(calls[1][1].first_message.mode,'user');assert.equal(calls[1][1].first_message.wait_timeout,false);
 d.getElementById('show-first-percent').value=101;
 d.getElementById('show-mode-form').dispatchEvent(new w.Event('submit',{cancelable:true}));await tick();
 assert.equal(calls.length,2);assert.match(d.getElementById('show-save-status').textContent,/0 to 100/);
 d.getElementById('show-first-percent').value=0;d.getElementById('show-first-min').value=0;
 d.getElementById('show-mode-form').dispatchEvent(new w.Event('submit',{cancelable:true}));await tick();
 assert.equal(calls.length,2);assert.match(d.getElementById('show-save-status').textContent,/positive minimum/);
 dom.window.close();
});
test('legacy first-message settings remain bot-first with no first-wait timeout',()=>{
 const {dom,w}=setup();
 for(const prefix of ['show','standard']) {
  w.fillFirstMessageSettings(prefix,undefined);const s=w.readFirstMessageSettings(prefix);
  assert.equal(s.mode,'bot');assert.equal(s.wait_timeout,false);assert.equal(s.bot_first_percent,70);
 }
 dom.window.close();
});

const savedShow = {first_message:{mode:'user',bot_first_percent:70,wait_timeout:true,wait_min_seconds:5,wait_max_seconds:8},reply_delay_min_seconds:3,reply_delay_max_seconds:6,auto_skip:true,no_reply_min_seconds:10,no_reply_max_seconds:20,next_chat_min_seconds:5,next_chat_max_seconds:8};
const readyRow = () => ({id:'show-1',profile:'Profile 1',account:'synthetic',status:'ready',detail:'Logged in — chat stopped',decision:'',message_mode:''});
test('dirty Show settings prevent Start and browser launch without touching Standard settings',async()=>{
 const {dom,w,d,calls}=setup();
 w.go.main.App.GetShowControlState=async()=>({revision:4,settings:savedShow,active:false,browsers:[readyRow()]});
 await w.refreshShowControl();
 assert.equal(d.querySelector('#show-sessions .primary-button').disabled,false);
 const input=d.getElementById('reply-delay-min');input.value=9;input.dispatchEvent(new w.Event('input',{bubbles:true}));
 assert.equal(d.querySelector('#show-sessions .primary-button').disabled,true);
 w.go.main.App.StartShowChat=async()=>calls.push(['start']);w.go.main.App.RunBrowser=async()=>calls.push(['open']);
 await w.controlShowChat('show-1',true);await w.runBrowserOption({key:'show_mode',label:'Show Mode'});
 assert.equal(calls.length,0);
 assert.match(d.getElementById('browser-status').textContent,/Save Show Mode/);
 await w.refreshShowControl();assert.equal(input.value,'9','poll discarded draft');
 dom.window.close();
});
test('Start sends saved revision; active summary uses backend settings and controls lock',async()=>{
 const {dom,w,d,calls}=setup();let active=false;
 w.go.main.App.GetShowControlState=async()=>({revision:7,settings:savedShow,active,browsers:[{...readyRow(),status:active?'running':'ready',settings:active?savedShow:undefined,decision:active?'User first':'',message_mode:active?'eva_chat_flow':''}]});
 w.go.main.App.StartShowChat=async(...args)=>{calls.push(['start',...args]);active=true;};
 w.go.main.App.StopShowChat=async id=>{calls.push(['stop',id]);active=false;};
 await w.refreshShowControl();await w.controlShowChat('show-1',true);
 assert.deepEqual(calls,[['start','show-1',7]]);
 assert.equal(d.getElementById('reply-delay-min').disabled,true);
 assert.equal(d.querySelector('#show-sessions .primary-button').disabled,true);
 assert.equal(d.querySelector('#show-sessions .secondary-button').disabled,false);
 assert.match(d.getElementById('show-sessions').textContent,/Running settings: User first.*Reply delay: 3–6s/);
 await w.controlShowChat('show-1',false);
 assert.deepEqual(calls[1],['stop','show-1']);assert.equal(d.getElementById('reply-delay-min').disabled,false);
 dom.window.close();
});
test('busy-account snapshot disables Start and renders untrusted labels as text',async()=>{
 const {dom,w,d}=setup();
 w.go.main.App.GetShowControlState=async()=>({revision:1,settings:savedShow,active:false,browsers:[{...readyRow(),profile:'<img src=x onerror=alert(1)>',blocked_by:'standard:1:test'}]});
 await w.refreshShowControl();
 assert.equal(d.querySelector('#show-sessions .primary-button').disabled,true);
 assert.equal(d.querySelector('#show-sessions img'),null);
 assert.match(d.getElementById('show-sessions').textContent,/standard:1:test/);
 dom.window.close();
});
test('late status response cannot overwrite a newer snapshot or saved revision',async()=>{
 const {dom,w,d}=setup();const pending=[];
 w.go.main.App.GetShowControlState=()=>new Promise(resolve=>pending.push(resolve));
 const first=w.refreshShowControl();const second=w.refreshShowControl();
 pending[1]({revision:9,settings:savedShow,active:false,browsers:[readyRow()]});await second;
 pending[0]({revision:8,settings:{...savedShow,reply_delay_min_seconds:0},active:true,browsers:[]});await first;
 assert.equal(d.getElementById('reply-delay-min').value,'3');
 assert.equal(d.getElementById('reply-delay-min').disabled,false);
 assert.equal(d.querySelector('#show-sessions .primary-button').disabled,false);
 dom.window.close();
});
