import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, Routes } from '@angular/router';
import { ReactiveFormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTimelineModule } from 'ng-zorro-antd/timeline';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { LLMPolicyComponent } from './llm-policy.component';
import { LLM_POLICY_SERVICE_TOKEN } from '../../services/llm-policy/llm-policy.service.token';
import { LLMPolicyService } from '../../services/llm-policy/llm-policy.service';

const routes: Routes = [{ path: '', component: LLMPolicyComponent }];

@NgModule({
  declarations: [LLMPolicyComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    ReactiveFormsModule,
    NzAlertModule,
    NzButtonModule,
    NzCardModule,
    NzCheckboxModule,
    NzEmptyModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzLayoutModule,
    NzTableModule,
    NzTagModule,
    NzTimelineModule,
    NzTooltipModule,
  ],
  providers: [{ provide: LLM_POLICY_SERVICE_TOKEN, useClass: LLMPolicyService }],
})
export class LLMPolicyModule {}
