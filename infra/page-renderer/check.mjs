// Передайте STAND_LAN_IP через окружение docker exec; результат не раскрывает адрес стенда.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import net from 'node:net';

const status = readFileSync('/proc/1/status', 'utf8');
assert.match(status, /^CapEff:\s+0+$/m);
assert.match(status, /^NoNewPrivs:\s+1$/m);
assert.doesNotMatch(status, /^Uid:\s+0\s/m);

async function reachable(host, port) {
  return new Promise(resolve => {
    const socket = net.connect({ host, port });
    const finish = result => { socket.destroy(); resolve(result); };
    socket.setTimeout(1500);
    socket.on('connect', () => finish(true));
    socket.on('error', () => finish(false));
    socket.on('timeout', () => finish(false));
  });
}

assert.ok(net.isIP(process.env.STAND_LAN_IP), 'set STAND_LAN_IP in the environment');
for (const [host, port, expected, label] of [
  [process.env.STAND_LAN_IP, 22, false, 'stand LAN'], ['169.254.169.254', 80, false, 'metadata'], ['1.1.1.1', 443, true, 'public HTTPS'],
]) {
  assert.equal(await reachable(host, port), expected, label);
}
const response = await fetch(`http://127.0.0.1:3000/content?token=${encodeURIComponent(process.env.TOKEN)}`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ url: 'https://example.com' }), signal: AbortSignal.timeout(30000),
}).catch(() => { throw new Error('renderer request failed'); });
assert.equal(response.status, 200);
assert.match(await response.text(), /Example Domain/);
console.log('PASS: unprivileged browser, LAN/metadata denied, public HTTPS rendered');
