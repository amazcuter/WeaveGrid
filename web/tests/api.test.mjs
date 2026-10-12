import test from 'node:test';
import assert from 'node:assert/strict';
import { api } from '../src/api.ts';

test('断线重发沿用幂等标识和预期版本，避免重复创建',async()=>{
 const original=globalThis.fetch;const requests=[];
 globalThis.fetch=async(url,options)=>{requests.push(structuredClone({url,...options}));if(requests.length===1)throw new TypeError('connection reset');return new Response('{"id":"only-one"}',{status:200})};
 try {const result=await api('/runs','POST',{mode:'backward'},4);assert.equal(result.id,'only-one');assert.equal(requests.length,2);assert.deepEqual(requests[0],requests[1]);assert.ok(requests[0].headers['Idempotency-Key']);assert.equal(requests[0].headers['If-Match'],'4')}finally{globalThis.fetch=original}
});
test('版本冲突不重试；服务未升级时返回可理解的提示',async()=>{
 const original=globalThis.fetch;let calls=0;
 try{globalThis.fetch=async()=>{calls++;return new Response('{"error":"版本已变化"}',{status:409})};await assert.rejects(()=>api('/projects/a','PUT',{}),/版本已变化/);assert.equal(calls,1);
 globalThis.fetch=async()=>new Response('not found',{status:404});await assert.rejects(()=>api('/projects/a/checkpoints'),/新版后台/)
 }finally{globalThis.fetch=original}
});
