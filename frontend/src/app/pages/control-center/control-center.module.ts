import { NgModule } from '@angular/core'
import { CommonModule } from '@angular/common'
import { RouterModule, Routes } from '@angular/router'
import { FormsModule } from '@angular/forms'
import { NzLayoutModule } from 'ng-zorro-antd/layout'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzModalModule } from 'ng-zorro-antd/modal'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NzSpinModule } from 'ng-zorro-antd/spin'
import { NzAlertModule } from 'ng-zorro-antd/alert'
import { ControlCenterComponent } from './control-center.component'
import { AUTOMATIONS_SERVICE_TOKEN } from '../../services/automations/automations.service.token'
import { AutomationsService } from '../../services/automations/automations.service'

const routes: Routes = [{ path: '', component: ControlCenterComponent }]

@NgModule({
  declarations: [ControlCenterComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    FormsModule,
    NzLayoutModule,
    NzTagModule,
    NzButtonModule,
    NzIconModule,
    NzModalModule,
    NzDrawerModule,
    NzSpinModule,
    NzAlertModule,
  ],
  providers: [
    { provide: AUTOMATIONS_SERVICE_TOKEN, useClass: AutomationsService },
  ],
})
export class ControlCenterModule {}
