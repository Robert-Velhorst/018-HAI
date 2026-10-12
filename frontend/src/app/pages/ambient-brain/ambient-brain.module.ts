import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputNumberModule } from 'ng-zorro-antd/input-number';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { AmbientBrainComponent } from './ambient-brain.component';

const routes: Routes = [{ path: '', component: AmbientBrainComponent }];

@NgModule({
  declarations: [AmbientBrainComponent],
  imports: [
    CommonModule,
    FormsModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    NzButtonModule,
    NzCheckboxModule,
    NzEmptyModule,
    NzIconModule,
    NzInputNumberModule,
    NzInputModule,
    NzLayoutModule,
    NzModalModule,
    NzRadioModule,
    NzTableModule,
    NzTagModule,
  ],
})
export class AmbientBrainModule {}
