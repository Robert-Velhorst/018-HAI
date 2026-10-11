import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ConnectedSourcesComponent } from './connected-sources.component';
import { CONNECTED_SOURCE_SERVICE_TOKEN } from '../../services/connected-source/connected-source.service.token';
import { ConnectedSourceService } from '../../services/connected-source/connected-source.service';

const routes: Routes = [{ path: '', component: ConnectedSourcesComponent }];

@NgModule({
  declarations: [ConnectedSourcesComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    FormsModule,
    ReactiveFormsModule,
    NzButtonModule,
    NzCardModule,
    NzCheckboxModule,
    NzEmptyModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzLayoutModule,
    NzModalModule,
    NzSelectModule,
    NzTableModule,
  ],
  providers: [
    { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useClass: ConnectedSourceService },
  ],
})
export class ConnectedSourcesModule {}
