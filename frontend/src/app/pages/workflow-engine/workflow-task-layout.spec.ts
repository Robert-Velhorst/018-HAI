import { Component, Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';

@Component({
  selector: 'hai-progressive-section', standalone: true,
  template: `<section class="hai-progressive-section"><button class="hai-progressive-section__summary">More controls</button><div class="hai-progressive-section__content"><ng-content /></div></section>`,
})
class DisclosureLayoutHarness {}

@Component({
  selector: 'app-workflow-engine', standalone: true, imports: [DisclosureLayoutHarness],
  template: `<section class="workflow-shell">
    <header style="display:flex;justify-content:flex-end"><hai-progressive-section class="workflow-more-tools"><div class="workflow-more-tools__body"><button>Follow-ups</button><button>Recover state</button></div></hai-progressive-section></header>
    <section class="action-strip" style="display:grid"><button class="action-card">Review approvals</button><button class="action-card">Run safe worker</button><button class="action-card">Capture signal</button></section>
    <section class="workflow-quiet"><span class="workflow-quiet__icon">I</span><div><h2>Start with a real-world signal</h2><p>Capture the next action.</p></div><button>Capture a signal</button></section>
    <main class="workflow-main workflow-main--idle"><section class="workflow-empty"><i>I</i><div><h2>No workflows match</h2><p>Adjust the filters.</p></div><button>Clear filters</button></section></main>
  </section>`,
  styleUrls: ['./workflow-engine.component.scss'],
})
class WorkflowLayoutHarness {}

@Component({
  selector: 'app-task-blueprint', standalone: true,
  template: `<section class="hai-chat-shell"><div class="hai-chat-layout is-advanced"><aside class="context-rail">Context</aside><main class="chat-main"><header class="chat-header"><div><h1>Plan the next action</h1></div><div class="view-mode-switch"><button aria-pressed="true">Basic</button><button>Advanced</button></div></header><article class="bubble"><p>Conversation</p></article></main><aside class="inspector"><section class="inspector-card"><p class="mcp-trace-note">Tool results require independent grounding.</p><ol class="mcp-trace-list"><li class="mcp-trace-row"><span class="mcp-trace-detail"><strong>Read source</strong><code>long-unbroken-source-reference-with-no-spaces-for-wrapping-checks</code></span><span class="mcp-trace-status">Completed</span><details class="mcp-trace-result" open><summary>Inspect result</summary><pre>Long-source-record-abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz</pre></details></li></ol></section></aside></div></section>`,
  styleUrls: ['../task-blueprint/task-blueprint.component.scss'],
})
class TaskLayoutHarness {}

describe('Workflow and task component stylesheet reconciliation', () => {
  async function withViewport(component: Type<unknown>, width: number, check: (doc: Document, view: Window) => void): Promise<void> {
    await TestBed.configureTestingModule({ imports: [component] }).compileComponents();
    const fixture = TestBed.createComponent(component);
    fixture.detectChanges();
    const frame = document.createElement('iframe');
    frame.title = 'Isolated workflow and task style check';
    frame.style.cssText = `position:fixed;top:0;left:0;width:${width}px;height:700px;border:0`;
    document.body.append(frame);
    try {
      const doc = frame.contentDocument!; const view = frame.contentWindow!;
      for (const sheet of Array.from(document.styleSheets)) {
        const style = doc.createElement('style');
        style.textContent = Array.from(sheet.cssRules).map(rule => rule.cssText).join('\n');
        doc.head.append(style);
      }
      const reset = doc.createElement('style');
      reset.textContent = 'body { margin:0; --hai-surface:#202124; --hai-text:#eeeeee; --hai-muted:#b9b9b9; --hai-border:#555555; } *, *::before, *::after { box-sizing:border-box; }';
      doc.head.prepend(reset);
      doc.body.append(fixture.nativeElement.cloneNode(true));
      expect(view.innerWidth).toBe(width);
      check(doc, view);
    } finally { frame.remove(); fixture.destroy(); }
  }

  for (const width of [375, 768, 1024, 1440]) {
    it(`applies empty/quiet layout to the component host at ${width}px`, async () => {
      await withViewport(WorkflowLayoutHarness, width, (doc, view) => {
        const quiet = doc.querySelector('.workflow-quiet') as HTMLElement;
        const empty = doc.querySelector('.workflow-empty') as HTMLElement;
        expect(view.getComputedStyle(quiet).display).toBe('grid');
        expect(view.getComputedStyle(empty).display).toBe('grid');
        expect(view.getComputedStyle(quiet.querySelector('h2')!).fontSize).toBe('16px');
        expect(view.getComputedStyle(quiet).backgroundColor).toBe('rgb(32, 33, 36)');
        expect((doc.querySelector('.workflow-quiet__icon') as HTMLElement).getBoundingClientRect().width).toBe(46);
        expect(view.getComputedStyle(doc.querySelector('.workflow-main')!).gridTemplateColumns.trim().split(/\s+/).length).toBe(1);
        expect(view.getComputedStyle(doc.querySelector('.action-strip')!).gridTemplateColumns.trim().split(/\s+/).length).toBe(width <= 680 ? 1 : width <= 980 ? 2 : 3);
        expect(doc.documentElement.scrollWidth).toBeLessThanOrEqual(width);
      });
    });
    it(`keeps only one positioned toolbar popover at ${width}px`, async () => {
      await withViewport(WorkflowLayoutHarness, width, (doc, view) => {
        const trigger = doc.querySelector('.workflow-more-tools') as HTMLElement;
        const content = doc.querySelector('.hai-progressive-section__content') as HTMLElement;
        const body = doc.querySelector('.workflow-more-tools__body') as HTMLElement;
        expect(trigger.getBoundingClientRect().width).toBe(148);
        expect(view.getComputedStyle(content).position).toBe('absolute');
        expect(view.getComputedStyle(body).position).toBe('static');
        expect(body.getBoundingClientRect().bottom).toBeLessThanOrEqual(content.getBoundingClientRect().bottom);
        expect(content.getBoundingClientRect().left).toBeGreaterThanOrEqual(0);
        expect(content.getBoundingClientRect().right).toBeLessThanOrEqual(width);
        expect(content.scrollWidth).toBeLessThanOrEqual(content.clientWidth);
      });
    });
    it(`keeps advanced task content bounded without implying a clickable note at ${width}px`, async () => {
      await withViewport(TaskLayoutHarness, width, (doc, view) => {
        const layout = doc.querySelector('.hai-chat-layout')!;
        expect(view.getComputedStyle(layout).gridTemplateColumns.trim().split(/\s+/).length).toBe(width <= 1080 ? 1 : 2);
        expect(view.getComputedStyle(doc.querySelector('.mcp-trace-note')!).cursor).not.toBe('pointer');
        expect(view.getComputedStyle(doc.querySelector('.mcp-trace-result summary')!).cursor).toBe('pointer');
        expect(view.getComputedStyle(doc.querySelector('.inspector-card')!).backgroundColor).toBe('rgb(32, 33, 36)');
        expect(doc.documentElement.scrollWidth).toBeLessThanOrEqual(width);
      });
    });
  }
});
