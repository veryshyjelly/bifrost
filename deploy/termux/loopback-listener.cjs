// Native deployment boundary equivalent to Compose's loopback port publication.
// Loaded only by the candidate Node launcher; does not alter outbound sockets.
'use strict';
const net = require('node:net');
const original = net.Server.prototype.listen;
function localHost(host) {
  if (host === undefined || host === '' || host === '0.0.0.0' || host === '::') return '127.0.0.1';
  if (host === '127.0.0.1' || host === '::1') return host;
  throw new Error('Native candidate listeners must use loopback');
}
net.Server.prototype.listen = function (...args) {
  const first = args[0];
  if (first && typeof first === 'object' && first.port !== undefined) {
    args[0] = { ...first, host: localHost(first.host) };
  } else if (typeof first === 'number' || (typeof first === 'string' && /^\d+$/.test(first))) {
    if (typeof args[1] === 'string') args[1] = localHost(args[1]);
    else args.splice(1, 0, '127.0.0.1');
  }
  return original.apply(this, args);
};
