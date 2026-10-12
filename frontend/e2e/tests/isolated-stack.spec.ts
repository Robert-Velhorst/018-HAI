import { expect, test } from '@playwright/test';
import { assertIsolatedAcceptanceTarget } from './support/isolated-stack';

test('acceptance targets require explicit isolation, a literal loopback port, and a synthetic owner', () => {
  const assert = (url: string | undefined, isolated = 'true', email = 'e2e-owner@example.test') =>
    assertIsolatedAcceptanceTarget(url, isolated, email);

  expect(() => assert('http://127.0.0.1:8080')).not.toThrow();
  expect(() => assert('https://[::1]:8443')).not.toThrow();
  for (const url of [undefined, '', 'http://localhost:8080', 'http://127.0.0.1',
    'http://127.0.0.1:80', 'https://[::1]:443', 'https://example.com:8080',
    'http://127.0.0.1:8080/api', 'http://127.0.0.1:8080/?x=1',
    'http://user:password@127.0.0.1:8080', 'file:///tmp/hai',
    'http://127.0.0.1:8080/#main']) {
    expect(() => assert(url), String(url)).toThrow();
  }
  for (const url of ['http://127.1:8080', 'http://2130706433:8080',
    'http://0x7f000001:8080', 'http://%31%32%37.0.0.1:8080',
    ' http://127.0.0.1:8080', 'http://127.0.0.1:8080\\']) {
    expect(() => assert(url), 'Nonliteral or normalized targets must be rejected').toThrow();
  }
  for (const isolated of [undefined, '', 'TRUE', '1']) {
    expect(() => assertIsolatedAcceptanceTarget('http://127.0.0.1:8080', isolated, 'e2e-owner@example.test')).toThrow();
  }
  expect(() => assert('http://127.0.0.1:8080', 'false')).toThrow();
  expect(() => assert('http://127.0.0.1:8080', 'true', 'robert@example.com')).toThrow();
});
