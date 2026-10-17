const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {JSDOM} = require('jsdom');
const base = path.join(__dirname,'../cmd/chitchatui/frontend/dist');
function setup() {
 const dom = new JSDOM(fs.readFileSync(path.join(base,'index.html'),'utf8'),{runScripts:'outside-only'});
 const w = dom.window; w.addEventListener=()=>{};
 const calls=[]; w.go={main:{App:{SaveShowModeSettings:async x=>calls.push(['show',x]),SaveGeneralSettings:async x=>calls.push(['standard',x])}}};
 w.eval(fs.readFileSync(path.join(base,'app.js'),'utf8'));
 w.bindForms();
 const d=w.document;
 for(const [id,value] of Object.entries({'reply-delay-min':3,'reply-delay-max':6,'show-timeout-min':30,'show-timeout-max':60,'show-next-min':2,'show-next-max':4,'worker-reply-delay-min':7,'worker-reply-delay-max':9,'poll-min':1,'poll-max':2,'action-min':1,'action-max':3,'match-reply-timeout-min':20,'match-reply-timeout-max':30,'match-close-wait-min':0,'match-close-wait-max':2})) d.getElementById(id).value=value;
 return {dom,w,d,calls};
}
const tick=()=>new Promise(r=>setImmediate(r));
test('settings sub-pages switch without mixing or clearing inputs',()=>{
 const {dom,d}=setup();
 assert.equal(d.getElementById('settings-standard').hidden,true);
 d.querySelector('[data-settings-tab="standard"]').click();
 assert.equal(d.getElementById('settings-show').hidden,true);
 assert.equal(d.getElementById('settings-standard').hidden,false);
 d.querySelector('[data-settings-tab="show"]').click();
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
 assert.match(d.getElementById('show-save-status').textContent,/restart Show Mode/);
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
