import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { parseTemplate } from '@angular/compiler'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const registryPath = resolve(frontendRoot, 'src/app/control-room/module-registry.ts')
const registryText = readFileSync(registryPath, 'utf8')
const registrySource = ts.createSourceFile(registryPath, registryText, ts.ScriptTarget.Latest, true)
const issues = []

function staticProperty(object, key) {
  return object.properties.find((property) => {
    if (!ts.isPropertyAssignment(property)) return false
    const name = property.name
    return (ts.isIdentifier(name) || ts.isStringLiteral(name)) && name.text === key
  })?.initializer
}

function staticString(node) {
  return node && (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node))
    ? node.text
    : undefined
}

function staticStringArray(node, label) {
  if (!node) return []
  if (!ts.isArrayLiteralExpression(node)) {
    issues.push(`${label}: expected a static array of section IDs.`)
    return []
  }

  return node.elements.flatMap((element) => {
    const value = staticString(element)
    if (value === undefined) issues.push(`${label}: section IDs must be static strings.`)
    return value === undefined ? [] : [value]
  })
}

function staticSectionPrefixes(node, label) {
  if (!node) return []
  if (!ts.isArrayLiteralExpression(node)) {
    issues.push(`${label}: expected a static array of dynamic section prefixes.`)
    return []
  }

  return node.elements.flatMap((element, index) => {
    if (!ts.isObjectLiteralExpression(element)) {
      issues.push(`${label}[${index}]: expected a static prefix definition.`)
      return []
    }

    const prefix = staticString(staticProperty(element, 'prefix'))
    const parentId = staticString(staticProperty(element, 'parentId'))
    if (!prefix || !parentId) {
      issues.push(`${label}[${index}]: prefix and parentId must be non-empty static strings.`)
      return []
    }

    return [{ prefix, parentId }]
  })
}

function staticStringMap(node, label) {
  if (!node) return {}
  if (!ts.isObjectLiteralExpression(node)) {
    issues.push(`${label}: expected a static section-parent map.`)
    return {}
  }

  const entries = []
  for (const property of node.properties) {
    if (!ts.isPropertyAssignment(property)) {
      issues.push(`${label}: section-parent entries must be static assignments.`)
      continue
    }

    const keyNode = property.name
    const key = ts.isIdentifier(keyNode) || ts.isStringLiteral(keyNode) ? keyNode.text : undefined
    const parentId = staticString(property.initializer)
    if (!key || !parentId) {
      issues.push(`${label}: child and parent IDs must be static strings.`)
      continue
    }
    entries.push([key, parentId])
  }
  return Object.fromEntries(entries)
}

function pageDirectory(route, moduleId) {
  if (route === '/home') return 'home'
  if (route === '/plans') return 'plan-coordination'
  return moduleId
}

function templateSectionIds(nodes, ids = []) {
  for (const node of nodes ?? []) {
    if (node.name === 'hai-progressive-section') {
      const sectionId = node.attributes.find((attribute) => attribute.name === 'sectionId')?.value
      if (sectionId) ids.push(sectionId)
    }
    if (node.name === 'details') {
      const sectionId = node.attributes.find((attribute) => attribute.name === 'data-hai-section')?.value
      if (sectionId) ids.push(sectionId)
    }
    templateSectionIds(node.children, ids)
  }
  return ids
}

function templateDynamicSectionPrefixes(nodes, prefixes = []) {
  for (const node of nodes ?? []) {
    if (node.name === 'hai-progressive-section') {
      const binding = node.inputs?.find((input) => input.name === 'sectionId')
      if (binding) {
        const expression = binding.value?.ast
        if (
          expression?.constructor?.name === 'Binary'
          && expression.operation === '+'
          && expression.left?.constructor?.name === 'LiteralPrimitive'
          && typeof expression.left.value === 'string'
          && expression.left.value.length > 0
          && expression.right
        ) {
          prefixes.push(expression.left.value)
        } else {
          issues.push(`${node.sourceSpan?.start?.toString() ?? 'template'}: dynamic sectionId must concatenate a static string prefix with a runtime value.`)
        }
      }
    }
    templateDynamicSectionPrefixes(node.children, prefixes)
  }
  return prefixes
}

let moduleArray
for (const statement of registrySource.statements) {
  if (!ts.isVariableStatement(statement)) continue
  for (const declaration of statement.declarationList.declarations) {
    if (
      ts.isIdentifier(declaration.name)
      && declaration.name.text === 'HAI_MODULES'
      && declaration.initializer
      && ts.isArrayLiteralExpression(declaration.initializer)
    ) {
      moduleArray = declaration.initializer.elements
    }
  }
}

if (!moduleArray?.length) {
  console.error('Could not read the HAI_MODULES registry.')
  process.exitCode = 1
} else {
  let checkedTemplates = 0
  let checkedSections = 0
  let checkedDynamicPrefixes = 0

  for (const entry of moduleArray) {
    if (!ts.isObjectLiteralExpression(entry)) continue

    const moduleId = staticString(staticProperty(entry, 'id'))
    const route = staticString(staticProperty(entry, 'route'))
    if (!moduleId || !route) {
      issues.push('Each module needs a static id and route.')
      continue
    }

    const directory = pageDirectory(route, moduleId)
    const templatePath = resolve(frontendRoot, `src/app/pages/${directory}/${directory}.component.html`)
    if (!existsSync(templatePath)) {
      issues.push(`${route}: main component template not found (${templatePath}).`)
      continue
    }

    const template = parseTemplate(readFileSync(templatePath, 'utf8'), templatePath)
    if (template.errors?.length) {
      issues.push(`${route}: Angular template parse failed: ${template.errors.map((error) => error.message).join('; ')}`)
      continue
    }

    checkedTemplates += 1
    const registered = new Set([
      ...staticStringArray(staticProperty(entry, 'advancedSectionIds'), `${route}.advancedSectionIds`),
      ...staticStringArray(staticProperty(entry, 'basicSectionIds'), `${route}.basicSectionIds`),
    ])
    const present = new Set(templateSectionIds(template.nodes))
    const registeredPrefixes = staticSectionPrefixes(
      staticProperty(entry, 'advancedSectionIdPrefixes'),
      `${route}.advancedSectionIdPrefixes`,
    )
    const parents = staticStringMap(staticProperty(entry, 'progressiveSectionParents'), `${route}.progressiveSectionParents`)
    const presentPrefixes = new Set(templateDynamicSectionPrefixes(template.nodes))
    checkedSections += present.size
    checkedDynamicPrefixes += presentPrefixes.size

    for (const id of registered) {
      if (!present.has(id)) issues.push(`${route}: registered section "${id}" is missing from its main template.`)
    }
    for (const id of present) {
      if (!registered.has(id)) issues.push(`${route}: template section "${id}" is missing from the module registry.`)
    }

    const seenPrefixes = new Set()
    for (const { prefix, parentId } of registeredPrefixes) {
      if (seenPrefixes.has(prefix)) issues.push(`${route}: dynamic section prefix "${prefix}" is registered more than once.`)
      seenPrefixes.add(prefix)
      if (!registered.has(parentId)) {
        issues.push(`${route}: dynamic section prefix "${prefix}" has unregistered parent "${parentId}".`)
      }
      if (!presentPrefixes.has(prefix)) {
        issues.push(`${route}: registered dynamic section prefix "${prefix}" is missing from its main template.`)
      }
    }
    for (const prefix of presentPrefixes) {
      if (!seenPrefixes.has(prefix)) {
        issues.push(`${route}: template dynamic section prefix "${prefix}" is missing from the module registry.`)
      }
    }

    for (const [child, parent] of Object.entries(parents)) {
      if (!registered.has(child)) issues.push(`${route}: progressive child section "${child}" is not registered.`)
      if (!registered.has(parent)) issues.push(`${route}: progressive parent section "${parent}" is not registered.`)
      if (child === parent) issues.push(`${route}: section "${child}" cannot be its own parent.`)
    }

    for (const child of Object.keys(parents)) {
      const seen = new Set()
      let current = child
      while (parents[current]) {
        if (seen.has(current)) {
          issues.push(`${route}: progressive section parent chain contains a cycle at "${current}".`)
          break
        }
        seen.add(current)
        current = parents[current]
      }
    }
  }

  if (issues.length) {
    console.error(`Progressive section contract failed with ${issues.length} issue(s):`)
    for (const issue of issues) console.error(`- ${issue}`)
    process.exitCode = 1
  } else {
    console.log(`Progressive section contract passed: ${checkedTemplates} module templates, ${checkedSections} static section IDs, ${checkedDynamicPrefixes} dynamic section prefixes.`)
  }
}
