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
  assert.match(section, /role="alert" \*ngIf="loadErrors\['selections'\]"/);
  assert.match(section, /<strong>Selection history unavailable<\/strong>/);
  assert.match(section, /\(click\)="refresh\(\)"[^>]*>Retry<\/button>/);
  assert.match(section, /<ng-container \*ngIf="selections\.length \|\| !loadErrors\['selections'\]">/);
  assert.match(section, /\*ngIf="selections\.length; else noSelectionHistory"/);
  assert.match(section, /<ng-template #noSelectionHistory>/);
  assert.ok(
    section.indexOf('<ng-template #noSelectionHistory>') > section.indexOf('<ng-container *ngIf="selections.length || !loadErrors[\'selections\']">'),
    'the empty state is inside the known-records or successful-read branch',
  );
});

test('selection history keeps known rows visible alongside a failed-refresh warning', () => {
  const section = template.match(/sectionId="selection-history"[\s\S]*?<\/hai-progressive-section>/)?.[0];
  assert.ok(section);
  assert.match(section, /\*ngIf="selections\.length">Previously loaded records remain visible/);
  assert.match(section, /<ng-container \*ngIf="selections\.length \|\| !loadErrors\['selections'\]">/);
  assert.match(section, /\[nzData\]="selections"/);
  assert.doesNotMatch(section, /loadErrors\['selections'\]; else selectionHistoryContent/);
});
