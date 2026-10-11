import { readdir, readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const sourceRoot = fileURLToPath(new URL('../src/app/', import.meta.url));
const themePath = new URL('../src/theme.less', import.meta.url);
const stylelessPackages = new Set(['i18n']);

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

const theme = await readFile(themePath, 'utf8');
const styledPackages = new Set(
  Array.from(theme.matchAll(/ng-zorro-antd\/([a-z0-9-]+)\/style\/entry\.less/g), (match) => match[1]),
);
const missing = Array.from(importedPackages).filter((name) => !styledPackages.has(name)).sort();

if (missing.length > 0) {
  console.error(`Missing NG-ZORRO theme styles: ${missing.join(', ')}`);
  process.exitCode = 1;
} else {
  console.log(`NG-ZORRO theme covers ${importedPackages.size} imported component packages.`);
}
