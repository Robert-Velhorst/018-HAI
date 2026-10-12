import { HAI_MODULES } from '../src/app/control-room/module-registry.ts'
import { loadComponentLogic } from './load-component-logic.mjs'

export function moduleUrlRegistryImport() {
  const registry = `export const HAI_MODULES = ${JSON.stringify(HAI_MODULES)}`
  return `data:text/javascript,${encodeURIComponent(registry)}`
}

export function bridgeModuleUrl(moduleForUrl) {
  globalThis.__haiTestModuleForUrl = moduleForUrl
  return 'data:text/javascript,export const moduleForUrl=(...args)=>globalThis.__haiTestModuleForUrl(...args)'
}

export function loadModuleUrlLogic() {
  return loadComponentLogic(new URL('../src/app/control-room/module-url.ts', import.meta.url), {
    './module-registry': moduleUrlRegistryImport(),
  })
}
