export function safeWebSourceHref(value: unknown, httpsOnly = false): string | undefined {
  if (typeof value !== 'string') return undefined;
  const uri = value.trim();
  if (!/^https?:\/\//i.test(uri) || uri.includes('\\') ||
      /[\u0000-\u001f\u007f]/.test(uri) || /%(?:0[0-9a-f]|1[0-9a-f]|7f)/i.test(uri)) return undefined;
  try {
    const parsed = new URL(uri);
    if ((parsed.protocol !== 'https:' && (httpsOnly || parsed.protocol !== 'http:')) ||
        !parsed.hostname || parsed.username || parsed.password) return undefined;
    return parsed.href;
  } catch {
    return undefined;
  }
}
