import { safeInternalReturnUrl, safePostOnboardingDestination } from './safe-return-url';

describe('safe return URLs', () => {
  it('preserves normalized internal paths, query strings, and fragments', () => {
    expect(safeInternalReturnUrl('/connected-sources?source=gmail#accounts'))
      .toBe('/connected-sources?source=gmail#accounts');
  });

  it('rejects external, protocol-relative, backslash, encoded-slash, and malformed URLs', () => {
    for (const value of [
      'https://example.com/path',
      '//example.com/path',
      '/\\\\example.com/path',
      '/%2f%2fevil.test/path',
      '/%5c%5cevil.test/path',
      '/%2e%2e//evil.test/path',
      '/%E0%A4%A',
    ]) {
      expect(safeInternalReturnUrl(value)).withContext(value).toBeNull();
    }
  });

  it('rejects login and onboarding destinations as post-onboarding return targets', () => {
    expect(safePostOnboardingDestination('/login?returnUrl=%2Fcontrol-center')).toBeNull();
    expect(safePostOnboardingDestination('/%6Fnboarding')).toBeNull();
    expect(safePostOnboardingDestination('/login;returnUrl=control-center')).toBeNull();
    expect(safePostOnboardingDestination('/onboarding/step')).toBeNull();
  });
});
