import { Component } from '@angular/core'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { provideRouter, Router, RouterOutlet } from '@angular/router'
import { NotFoundComponent } from './not-found.component'

@Component({
  standalone: true,
  imports: [RouterOutlet],
  template: '<router-outlet></router-outlet>',
})
class TestHostComponent {}

@Component({
  standalone: true,
  template: '<h1>Command Center test destination</h1>',
})
class CommandCenterStubComponent {}

describe('NotFoundComponent', () => {
  let fixture: ComponentFixture<NotFoundComponent>
  let router: Router

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [NotFoundComponent],
      providers: [
        provideRouter([
          { path: 'control-center', component: CommandCenterStubComponent },
        ]),
      ],
    }).compileComponents()

    fixture = TestBed.createComponent(NotFoundComponent)
    router = TestBed.inject(Router)
    fixture.detectChanges()
  })

  it('renders an accessible not-found message and a usable recovery link', () => {
    const heading = fixture.nativeElement.querySelector('h1') as HTMLHeadingElement
    const link = fixture.nativeElement.querySelector('a') as HTMLAnchorElement

    expect(heading.id).toBe('not-found-title')
    expect(heading.textContent).toContain('Page not found')
    expect(fixture.nativeElement.querySelector('section').getAttribute('aria-labelledby'))
      .toBe('not-found-title')
    expect(link.textContent.trim()).toBe('Go to Command Center')
    expect(link.getAttribute('href')).toBe('/control-center')
  })

  it('navigates the recovery link to the Command Center route', async () => {
    const link = fixture.nativeElement.querySelector('a') as HTMLAnchorElement
    link.click()
    await fixture.whenStable()

    expect(router.url).toBe('/control-center')
  })
})
