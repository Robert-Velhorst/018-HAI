import { ChangeDetectionStrategy, Component, Inject, OnInit, ViewChild } from '@angular/core'
import { CdkDragDrop, moveItemInArray } from '@angular/cdk/drag-drop'
import { FormGroup, FormControl, Validators, FormBuilder } from '@angular/forms'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { Router } from '@angular/router'
import { AUTOMATIONS_SERVICE_TOKEN } from '../../services/automations/automations.service.token'
import { IAutomationsService } from '../../services/automations.service.interface'
import { IAutomationModel } from '../../models/automation.model.interface'
import { AUTH_SERVICE_TOKEN } from '../../services/auth/auth.service.token'
import { IAuthService } from '../../services/auth.service.interface'
import { AutomationsFormComponent } from './modals/automations-form/automations-form.component'
import { NzModalService } from 'ng-zorro-antd/modal'
import { Subscription } from 'rxjs'
import { UserService } from '../../services/user/user.service'
import { USER_SERVICE_TOKEN } from '../../services/user/user.service.token'
import { IUserModel } from '../../models/user.model.interface'

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-home',
    templateUrl: './home.component.html',
    styleUrls: ['./home.component.scss'],
    standalone: false
})
export class HomeComponent implements OnInit {
  private userSubscription?: Subscription
  isProfileVisible = false
  profileForm: FormGroup = this.fb.group({})
  hidePassword: boolean = true
  hidePasswordConfirm: boolean = true
  automations: IAutomationModel[] = []
  automationLoadState: 'loading' | 'loaded' | 'error' = 'loading'
  automationReorderInFlight = false
  @ViewChild('automationModal', { static: false })
  automationModal!: AutomationsFormComponent

  constructor(
    private fb: FormBuilder,
    private notification: NzNotificationService,
    private modalService: NzModalService,
    @Inject(AUTOMATIONS_SERVICE_TOKEN)
    private automationsService: IAutomationsService,
    @Inject(AUTH_SERVICE_TOKEN) private authService: IAuthService,
    @Inject(USER_SERVICE_TOKEN) private userService: UserService,
    private router: Router
  ) {}

  ngOnInit() {
    this.onInitForm()
  }

  onInitForm() {
    this.profileForm = this.fb.group(
      {
        email: new FormControl('', {
          updateOn: 'blur',
          validators: [Validators.required, Validators.email],
        }),
        password: new FormControl('', {
          updateOn: 'blur',
          validators: [Validators.required],
        }),
        confirmPassword: new FormControl('', {
          updateOn: 'blur',
          validators: [Validators.required],
        }),
      },
      { validators: this.checkPasswords }
    )

    this.loadAutomations()
  }

  logout() {
    this.authService.logout().subscribe({
      next: () => {
        this.router.navigate(['/login']).then(() => {})
      },
      error: (err) => {
        this.notification.error('Error', 'There was an issue during logout.')
        console.error(err)
      },
    })
  }

  navigateFromMenu(route: string): void {
    this.router.navigate([route])
  }

  drop(event: CdkDragDrop<string[]>) {
    if (this.automationReorderInFlight) return
    const id1 = this.automations[event.previousIndex].id
    const id2 = this.automations[event.currentIndex].id

    if (!id1 || !id2 || id1 === id2) {
      return
    }
    const previousOrder = [...this.automations]
    moveItemInArray(this.automations, event.previousIndex, event.currentIndex)
    this.automationReorderInFlight = true
    this.automationsService.swapAutomations(id1, id2).subscribe({
      next: () => { this.automationReorderInFlight = false },
      error: () => {
        this.automations = previousOrder
        this.automationReorderInFlight = false
        this.notification.error('Reorder not confirmed', 'The request failed. HAI restored the previous order and is checking the saved registry.')
        this.loadAutomations()
      },
    })
  }

  handleFormDataAutomationSubmitted(automation: IAutomationModel) {
    if (this.automationModal?.submitting !== true) return
    if (this.automationModal.isUpdate) {
      this.automationsService.updateAutomation(automation).subscribe({
        next: () => {
          this.automationModal.completeSubmission()
          this.notification.success(
            'Success',
            'Automation updated successfully'
          )
          this.loadAutomations()
        },
        error: () => {
          this.automationModal.failSubmission()
          this.notification.error('Error', 'Error updating automation')
        },
      })
    } else {
      this.automationsService.addAutomation(automation).subscribe({
        next: () => {
          this.automationModal.completeSubmission()
          this.notification.success('Success', 'Automation added successfully')
          this.loadAutomations()
        },
        error: () => {
          this.automationModal.failSubmission()
          this.notification.error('Error', 'Error adding automation')
        },
      })
    }
  }

  showProfileModal(): void {
    this.userSubscription = this.userService.getUser().subscribe((user) => {
      this.profileForm.get('email')?.setValue(user.email)
      this.isProfileVisible = true
    })
  }

  onProfileSubmit(): void {
    if (!this.profileForm.valid) {
      for (const i in this.profileForm.controls) {
        this.profileForm.controls[i].markAsDirty()
        this.profileForm.controls[i].updateValueAndValidity()
      }
      return
    }

    const user: IUserModel = {
      email: this.profileForm.get('email')?.value,
      password: this.profileForm.get('password')?.value,
    }

    this.userSubscription = this.userService.updateUser(user).subscribe({
      next: () => {
        this.notification.success('Success', 'Profile updated successfully')
        this.loadAutomations()
        this.isProfileVisible = false
        this.profileForm.reset()
      },
      error: (err) => {
        this.notification.error(
          'Error',
          'There was an issue updating your profile.'
        )
        console.error(err)
      },
    })
  }

  handleProfileCancel(): void {
    this.isProfileVisible = false
    this.profileForm.reset()
    if (this.userSubscription) {
      this.userSubscription.unsubscribe()
      this.userSubscription = undefined
    }
  }

  checkPasswords(group: FormGroup) {
    // @ts-ignore
    let pass = group.get('password').value
    // @ts-ignore
    let confirmPass = group.get('confirmPassword').value

    return pass === confirmPass ? null : { notSame: true }
  }

  loadAutomations() {
    this.automationLoadState = 'loading'
    this.automationsService.getAutomations().subscribe({
      next: (automations: IAutomationModel[]) => {
        if (!Array.isArray(automations)) {
          this.automationLoadState = 'error'
          this.notification.error('Automation registry unavailable', 'The registry returned an unreadable response. HAI cannot confirm whether it is empty.')
          return
        }
        this.automations = [...automations].sort((a, b) => a.position - b.position)
        this.automationLoadState = 'loaded'
      },
      error: (error) => {
        this.automationLoadState = 'error'
        // registry a log
        console.error('Error fetching automations', error)
        this.notification.create(
          'error',
          'Error',
          'There was an error fetching the automations.'
        )
      },
    })
  }

  openAutomationModal(
    automation?: IAutomationModel,
    isUpdate: boolean = false
  ) {
    this.automationModal.openModal(automation, isUpdate)
  }

  deleteAutomation(automationId: string): void {
    this.modalService.confirm({
      nzTitle: 'Are you sure you want to delete this automation?',
      nzContent: 'This action cannot be undone.',
      nzOkText: 'Yes',
      nzOkType: 'primary',
      nzOnOk: () => {
        this.automationsService.deleteAutomation(automationId).subscribe({
          next: () => {
            this.loadAutomations()
            this.notification.create('success', 'Automation deleted', 'Automation deleted successfully.')
          },
          error: () => this.notification.error('Delete failed', 'The automation was not deleted. Try again.'),
        })
      },
      nzCancelText: 'No',
      nzOnCancel: () => {},
    })
  }
}
