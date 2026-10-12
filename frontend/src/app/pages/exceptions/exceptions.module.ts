import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ExceptionsComponent } from './exceptions.component';

const routes: Routes = [{ path: '', component: ExceptionsComponent }];

@NgModule({
  declarations: [ExceptionsComponent],
  imports: [
    CommonModule,
    FormsModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    NzTagModule,
    NzEmptyModule,
    NzButtonModule,
    NzSpinModule,
  ],
})
export class ExceptionsModule {}
