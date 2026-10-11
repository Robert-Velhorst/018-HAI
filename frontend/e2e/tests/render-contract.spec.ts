import { expect, test } from '@playwright/test';
import { readFileSync, readdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import ts from 'typescript';

const frontend = resolve(__dirname, '../..');

test('Angular component render strategies are explicit and legacy async rendering is enabled', () => {
  const implicit: string[] = [];
  let components = 0;
  function visitDirectory(directory: string): void {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const file = join(directory, entry.name);
      if (entry.isDirectory()) {
        visitDirectory(file);
      } else if (file.endsWith('.component.ts')) {
        const source = ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true);
        function visit(node: ts.Node): void {
          if (ts.isDecorator(node) && ts.isCallExpression(node.expression)
            && node.expression.expression.getText(source) === 'Component') {
            components++;
            const metadata = node.expression.arguments[0];
            if (!metadata || !ts.isObjectLiteralExpression(metadata)
              || !metadata.properties.some((property) => property.name?.getText(source) === 'changeDetection')) {
              implicit.push(file);
            }
          }
          ts.forEachChild(node, visit);
        }
        visit(source);
      }
    }
  }
  visitDirectory(join(frontend, 'src/app'));
  expect(components).toBeGreaterThan(30);
  expect(implicit, 'Angular 22 defaults to OnPush; every component must make a deliberate render choice').toEqual([]);
  const bootstrap = readFileSync(join(frontend, 'src/main.ts'), 'utf8');
  expect(bootstrap).toMatch(/applicationProviders:\s*\[provideZoneChangeDetection\(/);
});
