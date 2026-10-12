import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import ts from 'typescript'
import '@angular/compiler'

const require = createRequire(import.meta.url)

// Actual component logic and dependencies, without compiling/rendering its template.
export async function loadComponentLogic(sourceUrl, localImports = {}) {
  const compiled = ts.transpileModule(readFileSync(sourceUrl, 'utf8'), {
    compilerOptions: {
      module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022,
      experimentalDecorators: true, emitDecoratorMetadata: false,
      useDefineForClassFields: false, // Match the app's constructor-injection initialization policy.
    },
    reportDiagnostics: true,
  })
  assert.deepEqual(compiled.diagnostics, [])
  const emitted = ts.createSourceFile('component-logic.mjs', compiled.outputText, ts.ScriptTarget.ES2022, true, ts.ScriptKind.JS)
  const resolved = ts.transform(emitted, [context => root => ts.visitNode(root, function visit(node) {
    if (ts.isImportDeclaration(node)) {
      const specifier = node.moduleSpecifier.text
      let url = localImports[specifier]
      if (!url) {
        assert.ok(['@angular/core', '@angular/common', '@angular/common/http', '@angular/router', 'ng-zorro-antd/button',
          'ng-zorro-antd/icon', 'rxjs', 'rxjs/operators'].includes(specifier), `Unexpected runtime dependency: ${specifier}`)
        url = pathToFileURL(require.resolve(specifier)).href
      }
      return ts.factory.updateImportDeclaration(node, node.modifiers, node.importClause,
        ts.factory.createStringLiteral(url), node.attributes)
    }
    return ts.visitEachChild(node, visit, context)
  })])
  const code = `${ts.createPrinter().printFile(resolved.transformed[0])}\n//# sourceURL=${sourceUrl.href}.logic.mjs`
  resolved.dispose()
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`)
}
