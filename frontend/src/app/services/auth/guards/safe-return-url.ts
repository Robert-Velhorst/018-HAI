const INTERNAL_URL_BASE = 'http://hai.local';

/** Returns a normalized, same-origin app URL or null for an unsafe value. */
export function safeInternalReturnUrl(value: unknown): string | null {
  if (
    typeof value !== 'string' ||
    value.length === 0 ||
    value.length > 2048 ||
    value.trim() !== value ||
    !value.startsWith('/') ||
    value.startsWith('//') ||
    /[\\\u0000-\u001f\u007f]/.test(value)
  ) {
    return null;
  }

  const pathEnd = value.search(/[?#]/);
  const rawPath = pathEnd < 0 ? value : value.slice(0, pathEnd);
  let decodedPath = rawPath;
  try {
    for (let attempt = 0; attempt < 4; attempt += 1) {
      const decoded = decodeURIComponent(decodedPath);
      if (decoded === decodedPath) break;
      decodedPath = decoded;
    }
  } catch {
    return null;
  }

  if (
    !decodedPath.startsWith('/') ||
    decodedPath.startsWith('//') ||
    /[\\\u0000-\u001f\u007f]/.test(decodedPath)
  ) {
    return null;
  }

  try {
    const parsed = new URL(value, INTERNAL_URL_BASE);
    if (
      parsed.origin !== INTERNAL_URL_BASE ||
      parsed.username ||
      parsed.password ||
      parsed.pathname.startsWith('//') ||
      /[\\\u0000-\u001f\u007f]/.test(parsed.pathname)
    ) {
      return null;
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return null;
  }
}

/** Excludes destinations that would re-enter authentication or onboarding. */
export function safePostOnboardingDestination(value: unknown): string | null {
  const safeUrl = safeInternalReturnUrl(value);
  if (!safeUrl) return null;

  try {
    const path = decodeURIComponent(new URL(safeUrl, INTERNAL_URL_BASE).pathname)
      .replace(/\/+$/, '')
      .toLowerCase() || '/';
    const firstSegment = path.slice(1).split('/')[0].split(';')[0];
    return firstSegment === 'login' || firstSegment === 'onboarding' ? null : safeUrl;
  } catch {
    return null;
  }
}
