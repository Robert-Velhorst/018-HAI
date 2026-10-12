import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const template = await readFile(
  new URL('../src/app/pages/framework-registry/framework-registry.component.html', import.meta.url),
  'utf8',
);

test('selection history distinguishes an unavailable response from an empty history', () => {
  const section = template.match(
    /sectionId="selection-history"[\s\S]*?<\/hai-progressive-section>/,
  )?.[0];
  assert.ok(section, 'selection history section exists');
  assert.match(section, /loadErrors\['selections'\]; else selectionHistoryContent/);
  assert.match(section, /<strong>Selection history unavailable<\/strong>/);
  assert.match(section, /\(click\)="refresh\(\)"[^>]*>Retry<\/button>/);
  assert.match(section, /<ng-template #selectionHistoryContent>/);
  assert.match(section, /<ng-template #noSelectionHistory>/);
  assert.ok(
    section.indexOf('<ng-template #noSelectionHistory>') > section.indexOf('<ng-template #selectionHistoryContent>'),
    'the empty state is rendered only after a successful history response',
  );
});
