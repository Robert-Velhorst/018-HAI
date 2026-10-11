import { of } from 'rxjs'
import { OperationalGraphSnapshot } from '../../models/operational-graph.model'
import { OperationalBrainComponent } from './operational-brain.component'

describe('OperationalBrainComponent', () => {
  const snapshot: OperationalGraphSnapshot = {
    contractVersion: 1,
    generatedAt: '2026-08-13T12:00:00Z',
    rootId: 'hai:operational-root',
    nodes: [
      { id: 'hai:operational-root', kind: 'system', layer: 'system', label: 'HAI', weight: 1, localOnly: true, sourceCount: 0 },
      { id: 'workflow:1', kind: 'workflow', layer: 'work', label: 'Blocked reply', status: 'blocked', weight: .9, localOnly: true, sourceCount: 1 },
      { id: 'agent:planner', kind: 'agent', layer: 'agents', label: 'Planner', status: 'enabled', weight: .8, localOnly: true, sourceCount: 0 },
    ],
    links: [], layerCounts: { system: 1, work: 1, agents: 1 },
    quality: { orphanNodes: 0, sourceBackedNodes: 1, needsReviewNodes: 0, localOnlyNodes: 3, blockedNodes: 1 },
    truncated: false, scope: 'Owner scoped',
  }

  it('starts with the attention view and keeps ordinary records out', () => {
    const service = { snapshot: () => of(snapshot) }
    const component = new OperationalBrainComponent(service as never)
    component.ngOnInit()
    expect(component.visibleNodes.map((node) => node.id)).toEqual(['workflow:1'])
  })

  it('classifies verification and execution states consistently', () => {
    const component = new OperationalBrainComponent({} as never)
    expect(component.statusTone(snapshot.nodes[1])).toBe('danger')
    expect(component.statusTone(snapshot.nodes[2])).toBe('good')
    expect(component.displayStatus({ ...snapshot.nodes[2], verificationStatus: 'needs_review' })).toBe('needs_review')
    expect(component.displayStatus(snapshot.nodes[2])).toBe('enabled')
  })

  it('does not report system health when a search has no matches', () => {
    const component = new OperationalBrainComponent({} as never)
    component.searchQuery = 'missing record'
    expect(component.emptyStateTitle).toBe('No matching records')
    expect(component.emptyStateSummary).toContain('clear the search term')
    expect(component.emptyStateIcon).toBe('search')
  })
})
