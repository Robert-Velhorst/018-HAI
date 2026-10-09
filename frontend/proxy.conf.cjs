const { URL } = require('node:url');

const targetValue = process.env.HAI_API_PROXY_TARGET || 'http://127.0.0.1:8088';
const target = new URL(targetValue);
const loopbackHosts = new Set(['127.0.0.1', '::1', 'localhost']);
const targetHostname = target.hostname.replace(/^\[|\]$/g, '');

if (
  !['http:', 'https:'].includes(target.protocol) ||
  !loopbackHosts.has(targetHostname) ||
  target.pathname !== '/' ||
  target.search ||
  target.hash ||
  target.username ||
  target.password
) {
  throw new Error('HAI_API_PROXY_TARGET must be a loopback HTTP(S) origin without credentials or a path.');
}

module.exports = {
  '/api': {
    target: target.origin,
    changeOrigin: true,
    secure: target.protocol === 'https:',
  },
};
