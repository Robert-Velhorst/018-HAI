import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { parseTemplate, RecursiveAstVisitor, TmplAstRecursiveVisitor, tmplAstVisitAll } from '@angular/compiler'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const pagesRoot = resolve(frontendRoot, 'src/app/pages')
const loginPath = resolve(pagesRoot, 'login/login.component.html')
const hasMainRole = (value) => typeof value === 'string' && value.trim().split(/\s+/).includes('main')

export function mainLandmarks(source, filename = 'template.html') {
  const template = parseTemplate(source, filename)
  assert.equal(template.errors?.length ?? 0, 0,
    `${filename}: ${template.errors?.map((error) => error.message).join('; ')}`)
  const landmarks = []

  class RoleVisitor extends RecursiveAstVisitor {
    found = false
    visitLiteralPrimitive(ast) {
      this.found ||= hasMainRole(ast.value)
    }
  }

  class LandmarkVisitor extends TmplAstRecursiveVisitor {
    visitElement(element) {
      const role = new RoleVisitor()
      for (const binding of element.inputs) {
        if (binding.name === 'role') binding.value.visit(role)
      }
      if (element.name === 'main'
        || element.attributes.some((attribute) => attribute.name === 'role' && hasMainRole(attribute.value))
        || role.found) {
        landmarks.push(element)
      }
      super.visitElement(element)
    }
  }

  tmplAstVisitAll(new LandmarkVisitor(), template.nodes)
  return landmarks
}

test('operational page templates leave the main landmark to AppShell', () => {
  const paths = readdirSync(pagesRoot, { recursive: true })
    .filter((path) => path.endsWith('.html'))
    .map((path) => resolve(pagesRoot, path))
    .filter((path) => path !== loginPath)
  assert.ok(paths.length > 0)
  for (const path of paths) {
    const landmarks = mainLandmarks(readFileSync(path, 'utf8'), path)
    assert.deepEqual(landmarks.map((element) => element.sourceSpan.start.toString()), [],
      `${path}: operational pages must not declare <main> or role="main"`)
  }
})

test('login retains its own native main landmark outside AppShell', () => {
  const landmarks = mainLandmarks(readFileSync(loginPath, 'utf8'), loginPath)
  assert.equal(landmarks.length, 1)
  assert.equal(landmarks[0].name, 'main')
})

test('AppShell retains the sole authenticated main and skip-link target', () => {
  const path = resolve(frontendRoot, 'src/app/control-room/app-shell.component.html')
  const landmarks = mainLandmarks(readFileSync(path, 'utf8'), path)
  assert.equal(landmarks.length, 1)
  assert.equal(landmarks[0].name, 'main')
  assert.ok(landmarks[0].attributes.some((attribute) => attribute.name === 'id' && attribute.value === 'hai-main'))
})

test('parser checks structural directives, nested templates and control-flow branches', () => {
  const source = `<main *ngIf="visible"></main>
    <ng-template><div role="main"></div></ng-template>
    @if (visible) { <section><main></main></section> }
    @switch (mode) { @case ('active') { <main></main> } }
    @for (item of items; track item) { <main></main> }
    @defer { <main></main> } @placeholder { <main></main> }`
  assert.equal(mainLandmarks(source).length, 7)
})

test('parser detects static and conditional main role bindings', () => {
  assert.equal(mainLandmarks(`<div [attr.role]="'main'"></div>
    <section [role]="active ? 'main' : 'region'"></section>`).length, 2)
  assert.equal(mainLandmarks(`<main role="main"></main>`).length, 1)
})

test('comments, text and unrelated role bindings do not contribute landmarks', () => {
  assert.equal(mainLandmarks(`<!-- <main role="main"></main> -->
    <section aria-label="main"><p>&lt;main&gt;</p>
      <div [attr.role]="failed ? 'alert' : 'status'"></div>
    </section>`).length, 0)
})

test('malformed templates fail rather than silently bypassing the contract', () => {
  assert.throws(() => mainLandmarks('<section><main></section>'), /template.html/)
})
