import { Component, Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';

@Component({
  standalone: true,
  template: `<section class="sources-shell"><article class="source-row">
    <button class="source-row__select"><span class="source-row__icon">S</span>
      <span class="source-row__main"><strong>Connected source</strong><small>Last sync unavailable</small></span>
      <span class="source-row__badges"><span class="status-pill">Needs review</span></span>
    </button>
    <div class="source-row__actions"><button class="icon-button" aria-label="Sync">S</button><button class="icon-button" aria-label="Reindex">R</button></div>
  </article></section>`,
  styleUrls: ['./connected-sources.component.scss'],
})
class SourceStyleHarness {}

@Component({
  standalone: true,
  template: `<section class="action-detail"><dl class="action-detail__metrics"><div><dt>Status</dt><dd>Waiting</dd></div><div><dt>Owner</dt><dd>Robert</dd></div></dl></section>`,
  styleUrls: ['../control-center/control-center.component.scss'],
})
class ControlStyleHarness {}

describe('Source and control stylesheet reconciliation', () => {
  async function withViewport(component: Type<unknown>, width: number, check: (doc: Document, view: Window) => void): Promise<void> {
    await TestBed.configureTestingModule({ imports: [component] }).compileComponents();
    const fixture = TestBed.createComponent(component);
    fixture.detectChanges();
    const frame = document.createElement('iframe');
    frame.title = 'Isolated module stylesheet regression';
    frame.style.cssText = `position:fixed;top:0;left:0;width:${width}px;height:600px;border:0`;
    document.body.append(frame);
    try {
      const doc = frame.contentDocument!;
      const view = frame.contentWindow!;
      for (const sheet of Array.from(document.styleSheets)) {
        const style = doc.createElement('style');
        style.textContent = Array.from(sheet.cssRules).map(rule => rule.cssText).join('\n');
        doc.head.append(style);
      }
      const reset = doc.createElement('style');
      reset.textContent = 'body { margin: 0; } *, *::before, *::after { box-sizing: border-box; }';
      doc.head.prepend(reset);
      doc.body.append(fixture.nativeElement.cloneNode(true));
      expect(view.innerWidth).toBe(width);
      check(doc, view);
    } finally {
      frame.remove();
      fixture.destroy();
    }
  }

  for (const width of [375, 768, 1024, 1440]) {
    it(`keeps source actions in the explicit single registry column at ${width}px`, async () => {
      await withViewport(SourceStyleHarness, width, (doc, view) => {
        const row = doc.querySelector('.source-row') as HTMLElement;
        const select = doc.querySelector('.source-row__select') as HTMLElement;
        const actions = doc.querySelector('.source-row__actions') as HTMLElement;
        expect(view.getComputedStyle(row).gridTemplateColumns.trim().split(/\s+/).length).toBe(1);
        expect(actions.getBoundingClientRect().top).toBeGreaterThanOrEqual(select.getBoundingClientRect().bottom);
        expect(row.scrollWidth).toBeLessThanOrEqual(row.clientWidth);
        for (const button of Array.from(actions.querySelectorAll('button'))) {
          const box = button.getBoundingClientRect();
          expect(box.width).toBeGreaterThanOrEqual(32);
          expect(box.left).toBeGreaterThanOrEqual(row.getBoundingClientRect().left);
          expect(box.right).toBeLessThanOrEqual(row.getBoundingClientRect().right);
        }
      });
    });
    it(`respects the inspector metric breakpoint at ${width}px`, async () => {
      await withViewport(ControlStyleHarness, width, (doc, view) => {
        const metrics = doc.querySelector('.action-detail__metrics') as HTMLElement;
        const columns = view.getComputedStyle(metrics).gridTemplateColumns.trim().split(/\s+/).length;
        expect(columns).toBe(width <= 720 ? 1 : 2);
        expect(doc.documentElement.scrollWidth).toBeLessThanOrEqual(width);
      });
    });
  }
});
