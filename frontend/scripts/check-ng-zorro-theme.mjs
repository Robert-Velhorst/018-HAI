import { readdir, readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const sourceRoot = fileURLToPath(new URL('../src/app/', import.meta.url));
const themePaths = [
  new URL('../src/theme.less', import.meta.url),
  new URL('../src/authenticated-theme.less', import.meta.url),
];
const stylelessPackages = new Set(['i18n']);
const publicThemePackages = new Set([
  'button', 'card', 'checkbox', 'form', 'icon', 'input', 'layout', 'notification',
]);

async function typescriptFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(entries.map(async (entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return typescriptFiles(path);
    return extname(entry.name) === '.ts' ? [path] : [];
  }));
  return nested.flat();
}

const importedPackages = new Set();
for (const path of await typescriptFiles(sourceRoot)) {
  const source = await readFile(path, 'utf8');
  for (const match of source.matchAll(/from\s+['"]ng-zorro-antd\/([a-z0-9-]+)['"]/g)) {
    if (!stylelessPackages.has(match[1])) importedPackages.add(match[1]);
  }
}

const themes = await Promise.all(themePaths.map((path) => readFile(path, 'utf8')));
const packagesInTheme = (theme) => new Set(
  Array.from(theme.matchAll(/ng-zorro-antd\/([a-z0-9-]+)\/style\/entry\.less/g), (match) => match[1]),
);
const initialPackages = packagesInTheme(themes[0]);
const authenticatedPackages = packagesInTheme(themes[1]);
const missing = Array.from(importedPackages).filter((name) =>
  !initialPackages.has(name) && !authenticatedPackages.has(name),
).sort();
const missingPublic = Array.from(publicThemePackages).filter((name) => !initialPackages.has(name));
const misplacedAuthenticated = Array.from(importedPackages).filter((name) =>
  !publicThemePackages.has(name) && !authenticatedPackages.has(name),
);
const unexpectedInitial = Array.from(initialPackages).filter((name) => !publicThemePackages.has(name));

if (missing.length > 0 || missingPublic.length > 0 || misplacedAuthenticated.length > 0 || unexpectedInitial.length > 0) {
  if (missing.length > 0) console.error(`Missing NG-ZORRO theme styles: ${missing.join(', ')}`);
  if (missingPublic.length > 0) console.error(`Login-required NG-ZORRO styles must stay in theme.less: ${missingPublic.join(', ')}`);
  if (misplacedAuthenticated.length > 0) console.error(`Authenticated NG-ZORRO styles are missing from authenticated-theme.less: ${misplacedAuthenticated.join(', ')}`);
  if (unexpectedInitial.length > 0) console.error(`Non-login NG-ZORRO styles are in the initial theme: ${unexpectedInitial.join(', ')}`);
  process.exitCode = 1;
} else {
  console.log(`Split NG-ZORRO themes cover ${importedPackages.size} packages; ${publicThemePackages.size} login packages remain in the initial theme.`);
}
