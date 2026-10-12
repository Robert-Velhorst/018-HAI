export function assertIsolatedAcceptanceTarget(
  baseURL: string | undefined,
  isolatedStack: string | undefined,
  operatorEmail: string,
): void {
  if (isolatedStack !== 'true') {
    throw new Error('Authenticated acceptance requires E2E_ISOLATED_STACK=true for a disposable HAI stack.');
  }
  // URL parsing normalizes integer, shortened and encoded hosts to loopback.
  // Require the operator's input itself to name the literal approved host.
  if (!/^https?:\/\/(?:127\.0\.0\.1|\[::1\]):[0-9]+\/?$/.test(baseURL || '')) {
    throw new Error('Authenticated acceptance requires a literal loopback URL with an explicit port.');
  }
  let target: URL;
  try {
    target = new URL(baseURL || '');
  } catch {
    throw new Error('Authenticated acceptance requires an explicit loopback E2E_BASE_URL.');
  }
  const host = target.hostname.replace(/^\[|\]$/g, '');
  if (!['http:', 'https:'].includes(target.protocol)
    || !['127.0.0.1', '::1'].includes(host)
    || target.username || target.password || target.pathname !== '/'
    || target.search || target.hash || !target.port
    || ['80', '443'].includes(target.port)) {
    throw new Error('Authenticated acceptance must use an explicit non-default port on literal loopback, not the shared HAI installation.');
  }
  if (!/^e2e-[a-z0-9._-]+@example\.test$/i.test(operatorEmail)) {
    throw new Error('Authenticated acceptance requires a synthetic e2e-* operator at example.test.');
  }
}
