// Run inside the image build with --network=none. The only model is this
// synthetic loopback fixture. This proves CLI/config/result compatibility,
// not model quality, safety effectiveness or real provider acceptance.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { ENGINE, runEvaluation } from './app.mjs';

let requests = 0;
let rejectedRequests = 0;
const server = createServer(async (request, response) => {
  try {
    assert.equal(request.method, 'POST');
    assert.equal(request.url, '/v1/chat/completions');
    let body = '';
    for await (const chunk of request) {
      body += chunk.toString('utf8');
      assert.ok(Buffer.byteLength(body) <= 64 * 1024);
    }
    const payload = JSON.parse(body);
    assert.equal(payload.model, 'synthetic-cli-compatibility');
    assert.ok(Array.isArray(payload.messages) && payload.messages.length > 0);
    requests++;
    response.writeHead(200, { 'content-type':'application/json' });
    response.end(JSON.stringify({id:'synthetic',object:'chat.completion',created:0,model:payload.model,choices:[{index:0,message:{role:'assistant',content:'REFUSE_AND_REQUEST_APPROVAL synthetic compatibility fixture'},finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}));
  } catch {
    rejectedRequests++;
    response.writeHead(400, { 'content-type':'application/json' });
    response.end('{"error":{"message":"synthetic fixture request rejected"}}');
  }
});
server.listen(0, '127.0.0.1');
await once(server, 'listening');
try {
  const port = server.address().port;
  const result = await runEvaluation({HAI_PROMPTFOO_MODEL_ID:'synthetic-cli-compatibility',HAI_PROMPTFOO_MODEL_BASE_URL:`http://127.0.0.1:${port}/v1`});
  assert.equal(result.engine, ENGINE);
  assert.equal(result.status, 'completed');
  assert.equal(result.caseCount, 6); assert.equal(result.passedCount, 6); assert.equal(result.failedCount, 0);
  assert.equal(requests, 6);
  assert.equal(rejectedRequests, 0);
  assert.match(result.resultDigest, /^[a-f0-9]{64}$/);
  console.log(JSON.stringify({engine:ENGINE,fixtureRequests:requests,cases:result.caseCount,scope:'synthetic loopback CLI compatibility only',realModelAcceptance:false,operatorDataTouched:false}));
} finally {
  server.closeAllConnections();
  await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
}
