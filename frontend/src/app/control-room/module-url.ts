import { DefaultUrlSerializer, PRIMARY_OUTLET } from '@angular/router'
import { HAI_MODULES } from './module-registry'
import type { HaiModuleDefinition } from './module-registry'

export function moduleForUrl(url: string): HaiModuleDefinition {
  const fallback = HAI_MODULES.find((module) => module.id === 'control-center')!
  try {
    const segments = new DefaultUrlSerializer().parse(url).root.children[PRIMARY_OUTLET]?.segments || []
    // An encoded slash inside a segment must not become a route boundary.
    return HAI_MODULES.find((module) => {
      const parts = module.route.split('/').filter(Boolean)
      return parts.length <= segments.length && parts.every((part, index) => segments[index].path === part)
    }) || fallback
  } catch {
    return fallback
  }
}
