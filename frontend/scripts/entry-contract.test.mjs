import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import ts from 'typescript'

const read = (path) => readFileSync(new URL(`../src/app/${path}`, import.meta.url), 'utf8')
const routesText = read('app-routing.module.ts')
const legacyPaths = [
  'onboarding', 'home', 'command-dashboard', 'ambient-brain', 'hai-os', 'life-ops',
  'framework-registry', 'control-center', 'llm-policy', 'memory', 'task-blueprint',
  'connected-sources', 'grounded-answers', 'workflow-engine', 'pursuits',
  'background-operations', 'model-intelligence', 'runtime-lab', 'account-bridges',
  'runtime-control', 'plans', 'agent-teams', 'knowledge-claims', 'governance-control',
  'exceptions', 'quick-capture', 'system-status', 'brain-catalog', 'skills',
]
const originalIcons = [
  'BulbOutline', 'CalendarOutline', 'ContactsOutline', 'DisconnectOutline',
  'HeartOutline', 'NodeIndexOutline', 'ReloadOutline', 'StarOutline',
  'WarningFill', 'WarningOutline',
]

function parse(text) {
  const source = ts.createSourceFile('entry.ts', text, ts.ScriptTarget.Latest, true)
  assert.deepEqual(source.parseDiagnostics, [], 'entry syntax must remain parseable')
  return source
}

function array(source, name) {
  let result
  function visit(node) {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === name) {
      assert.equal(result, undefined, `duplicate ${name} declaration`)
      assert.ok(node.initializer && ts.isArrayLiteralExpression(node.initializer), `${name} must be explicit`)
      result = node.initializer.elements
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  assert.ok(result, `${name} declaration missing`)
  return result
}

function properties(node) {
  assert.ok(ts.isObjectLiteralExpression(node), 'route must be an explicit object')
  return new Map(node.properties.map(property => {
    assert.ok(ts.isPropertyAssignment(property), 'route property must be explicit')
    return [property.name.text, property.initializer]
  }))
}

const identifiers = node => {
  assert.ok(node && ts.isArrayLiteralExpression(node), 'guard/icon list must be explicit')
  return node.elements.map(item => { assert.ok(ts.isIdentifier(item)); return item.text })
}

function validateRoutes(text) {
  const source = parse(text)
  const routes = array(source, 'authenticatedRoutes').map(properties)
  const byPath = new Map()
  for (const route of routes) {
    assert.ok(route.get('path') && ts.isStringLiteral(route.get('path')))
    const path = route.get('path').text
    assert.ok(!byPath.has(path), `duplicate route: ${path}`)
    assert.ok(identifiers(route.get('canActivate')).includes('authGuard'), `${path} must remain guarded`)
    byPath.set(path, route)
  }
  for (const path of [...legacyPaths, 'operational-brain', 'hire-ai']) {
    const route = byPath.get(path)
    assert.ok(route, `retained URL missing: ${path}`)
    const modules = []
    function visit(node) {
      if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
        assert.ok(ts.isStringLiteral(node.arguments[0]))
        modules.push(node.arguments[0].text)
      }
      ts.forEachChild(node, visit)
    }
    assert.ok(route.get('loadChildren'), `${path} must retain a lazy module`)
    visit(route.get('loadChildren'))
    const module = path === 'plans' ? 'plan-coordination' : path
    assert.deepEqual(modules, [`./pages/${module}/${module}.module`], `${path} lazy target changed`)
  }
  return source
}

test('independent retained URL contract preserves lazy modules and authentication guards', () => {
  validateRoutes(routesText)
})

test('route contract rejects missing URLs, removed guards and changed lazy targets', () => {
  assert.throws(() => validateRoutes(routesText.replace('path: "home"', 'path: "removed-home"')), /retained URL missing/)
  assert.throws(() => validateRoutes(routesText.replace('canActivate: [authGuard]', 'canActivate: []')), /must remain guarded/)
  assert.throws(() => validateRoutes(routesText.replace('./pages/home/home.module', './pages/login/login.module')), /lazy target changed/)
  assert.throws(() => validateRoutes(routesText.replace('path: "home"', 'path: "onboarding"')), /duplicate route/)
})

test('shell retains authenticated theme loading while login stays outside operational navigation', () => {
  const routes = array(validateRoutes(routesText), 'APP_ROUTES').map(properties)
  const login = routes.find(route => route.get('path')?.text === 'login')
  const shell = routes.find(route => route.get('path')?.text === '')
  assert.ok(login && shell)
  assert.ok(identifiers(login.get('canActivate')).includes('RedirectIfLoggedGuard'))
  assert.equal(login.has('canActivateChild'), false)
  assert.ok(identifiers(shell.get('canActivateChild')).includes('authenticatedThemeGuard'))
  const children = shell.get('children')
  assert.ok(ts.isArrayLiteralExpression(children))
  assert.ok(children.elements.some(node => ts.isSpreadElement(node) && ts.isIdentifier(node.expression) && node.expression.text === 'authenticatedRoutes'))
})

test('central icon registration retains every original global icon', () => {
  const icons = array(parse(read('hai-icons.ts')), 'HAI_ICONS').map(node => { assert.ok(ts.isIdentifier(node)); return node.text })
  assert.equal(new Set(icons).size, icons.length, 'shared icons must not be duplicated')
  for (const icon of originalIcons) assert.ok(icons.includes(icon), `original shared icon missing: ${icon}`)
})

test('root module provides and reexports the central icons and imports the control room', () => {
  const source = parse(read('app.module.ts'))
  let metadata
  function visit(node) {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'NgModule') {
      assert.equal(metadata, undefined, 'root module metadata must be unique')
      metadata = properties(node.arguments[0])
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  assert.ok(metadata)
  assert.ok(identifiers(metadata.get('imports')).includes('ControlRoomModule'))
  const providers = metadata.get('providers')
  assert.ok(ts.isArrayLiteralExpression(providers))
  const iconProvider = providers.elements.filter(ts.isObjectLiteralExpression).map(properties)
    .find(provider => provider.get('provide')?.text === 'NZ_ICONS')
  assert.equal(iconProvider?.get('useValue')?.text, 'HAI_ICONS')
  assert.ok(source.statements.some(node => ts.isExportDeclaration(node) && node.moduleSpecifier?.text === './hai-icons' &&
    node.exportClause && ts.isNamedExports(node.exportClause) && node.exportClause.elements.some(item => item.name.text === 'HAI_ICONS')))
})
