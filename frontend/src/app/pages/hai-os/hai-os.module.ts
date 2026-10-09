import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzTableModule } from 'ng-zorro-antd/table';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { HAIOSComponent } from './hai-os.component';
import { HAI_OS_SERVICE_TOKEN } from '../../services/hai-os/hai-os.service.token';
import { HAIOSService } from '../../services/hai-os/hai-os.service';

const routes: Routes = [{ path: '', component: HAIOSComponent }];

@NgModule({
  declarations: [HAIOSComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    NzButtonModule,
    NzTableModule,
  ],
  providers: [{ provide: HAI_OS_SERVICE_TOKEN, useClass: HAIOSService }],
})
export class HAIOSModule {}
