import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { QuickCaptureComponent } from './quick-capture.component';

const routes: Routes = [{ path: '', component: QuickCaptureComponent }];

@NgModule({
  declarations: [QuickCaptureComponent],
  imports: [
    CommonModule,
    ReactiveFormsModule,
    RouterModule.forChild(routes),
    NzFormModule,
    NzInputModule,
    NzButtonModule,
    NzCardModule,
    ControlRoomModule,
  ],
})
export class QuickCaptureModule {}
