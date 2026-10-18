import { CommonModule } from '@angular/common'
import { provideHttpClient } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { provideNoopAnimations } from '@angular/platform-browser/animations'
import { Router } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzCardModule } from 'ng-zorro-antd/card'
import { NzEmptyModule } from 'ng-zorro-antd/empty'
import { NzIconModule, provideNzIcons } from 'ng-zorro-antd/icon'
import { DownOutline, UpOutline } from '@ant-design/icons-angular/icons'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { NzTableModule } from 'ng-zorro-antd/table'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { IFeed } from '../../models/account-bridges.model.interface'
import { AccountBridgesComponent } from './account-bridges.component'

describe('Account Bridges rendered source controls', () => {
  let fixture: ComponentFixture<AccountBridgesComponent>
  let http: HttpTestingController
  let viewPreferences: ModuleViewPreferencesService
  const source: IFeed = {
    id: 'source-1', name: 'Autoposter', provider: 'secondhand_autoposter',
    accountLabel: 'Synthetic owner', sourceType: 'autoposter_api', enabled: true,
  }
  const api = '/api/v1/account-feeds'

  const flushRefresh = async (enabled: boolean) => {
    http.expectOne(`${api}/bridges`).flush({ bridges: [] })
    http.expectOne(api).flush({ feeds: [{ feed: { ...source, enabled }, connectionStatus: 'available', syncState: 'idle', lastItemsRead: 0 }] })
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
  }
  const button = (label: string): HTMLButtonElement => fixture.nativeElement.querySelector(`button[aria-label="${label}"]`)

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [AccountBridgesComponent],
      imports: [CommonModule, ControlRoomModule, NzButtonModule, NzCardModule, NzEmptyModule, NzIconModule, NzTableModule, NzTagModule],
      providers: [provideHttpClient(), provideHttpClientTesting(), provideNoopAnimations(), provideNzIcons([DownOutline, UpOutline]),
        { provide: Router, useValue: { navigate: jasmine.createSpy('navigate') } },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('notification', ['success', 'error', 'warning']) }],
    }).compileComponents()
    http = TestBed.inject(HttpTestingController)
    viewPreferences = TestBed.inject(ModuleViewPreferencesService)
    viewPreferences.setMode('account-bridges', 'advanced')
    viewPreferences.setSection('account-bridges', 'registered-feeds', true)
    fixture = TestBed.createComponent(AccountBridgesComponent)
    fixture.detectChanges()
    await flushRefresh(true)
  })

  afterEach(() => {
    http.verify()
    viewPreferences.reset('account-bridges')
  })

  it('connects native Disable/Enable buttons to PATCH and blocks Sync while disabled', async () => {
    expect(button('Disable Autoposter')).not.toBeNull()
    button('Disable Autoposter').click()
    fixture.detectChanges()
    const disable = http.expectOne(`${api}/${source.id}`)
    expect(disable.request.method).toBe('PATCH')
    expect(disable.request.body).toEqual({ enabled: false })
    expect(button('Disable Autoposter').disabled).toBeTrue()
    disable.flush({ ...source, enabled: false })
    await flushRefresh(false)
    const sync = Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((entry) => entry.textContent?.trim() === 'Sync now')!
    expect(sync.disabled).toBeTrue()
    sync.click()
    http.expectNone(`${api}/${source.id}/sync`)
    button('Enable Autoposter').click()
    const enable = http.expectOne(`${api}/${source.id}`)
    expect(enable.request.method).toBe('PATCH')
    expect(enable.request.body).toEqual({ enabled: true })
    enable.flush({ ...source, enabled: true })
    await flushRefresh(true)
    expect(button('Disable Autoposter')).not.toBeNull()
    const enabledSync = fixture.nativeElement.querySelector('button[aria-label="Sync now: Autoposter"]') as HTMLButtonElement
    expect(enabledSync.disabled).toBeFalse()
    http.expectNone((req) => req.method === 'POST') // Enable never starts a sync.
    enabledSync.click()
    const run = http.expectOne(`${api}/${source.id}/sync`)
    expect(run.request.method).toBe('POST')
    run.flush({ feedId: source.id, itemsRead: 1, operationsCreated: 0, privacyFlagged: 0, errors: [], recorded: true })
    await flushRefresh(true)
  })
})
