import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { GroundedAnswersComponent } from './grounded-answers.component';
import { VERIFICATION_SERVICE_TOKEN } from '../../services/verification/verification.service.token';
import { VerificationService } from '../../services/verification/verification.service';

const routes: Routes = [{ path: '', component: GroundedAnswersComponent }];

@NgModule({
  declarations: [GroundedAnswersComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    ReactiveFormsModule,
    NzButtonModule,
    NzCardModule,
    NzCheckboxModule,
    NzEmptyModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzSelectModule,
    NzTableModule,
  ],
  providers: [{ provide: VERIFICATION_SERVICE_TOKEN, useClass: VerificationService }],
})
export class GroundedAnswersModule {}
