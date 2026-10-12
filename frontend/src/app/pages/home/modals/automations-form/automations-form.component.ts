import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, EventEmitter, inject, Input, OnInit, Output } from '@angular/core'
import { takeUntilDestroyed } from '@angular/core/rxjs-interop'
import { FormBuilder, FormGroup, Validators } from '@angular/forms'
import { NzUploadChangeParam, NzUploadFile } from 'ng-zorro-antd/upload'
import { IAutomationModel } from '../../../../models/automation.model.interface'
import { NzNotificationService } from 'ng-zorro-antd/notification'

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-automations-form-modal',
    templateUrl: './automations-form.component.html',
    styleUrls: ['./automations-form.component.scss'],
    standalone: false
})
export class AutomationsFormComponent implements OnInit {
  private readonly destroyRef = inject(DestroyRef)
  private readonly changeDetector = inject(ChangeDetectorRef)
  modalTitle: string = ''
  removeImage: boolean = false
  selectedImage: File | undefined
  @Input() isUpdate: boolean = false
  @Input() isVisible: boolean = false
  @Input() uploadUrl: string = ''
  submitting = false
  @Output() modalClosed = new EventEmitter<void>()
  @Output() formDataSubmitted = new EventEmitter<IAutomationModel>()
  automationForm!: FormGroup

  constructor(
    private fb: FormBuilder,
    private notification: NzNotificationService
  ) {}

  ngOnInit() {
    this.onInitForm()
  }

  onInitForm() {
    this.automationForm = this.fb.group({
      id: [null],
      name: [
        '',
        { validators: [Validators.required, Validators.maxLength(50)] },
      ],
      host: [
        '',
        { validators: [Validators.required, Validators.maxLength(50)] },
      ],
      port: [
        '',
        {
          validators: [
            Validators.required,
            Validators.min(1),
            Validators.max(65535),
          ],
        },
      ],
      launchType: ['browser_url'],
      launchTarget: [''],
      runtimeType: ['browser'],
      runtimeModel: ['', [Validators.maxLength(255), Validators.pattern(/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.:/-]+$/)]],
      serviceName: [''],
      routePath: [''],
      publicUrl: [''],
      localUrl: [''],
      healthCheckType: ['http'],
      healthCheckUrl: [''],
      healthCheckIntervalSeconds: [60, { validators: [Validators.min(0)] }],
      expectedHttpStatus: [200, { validators: [Validators.min(100), Validators.max(599)] }],
      dependencyNotes: [''],
    })
    this.automationForm.valueChanges.pipe(takeUntilDestroyed(this.destroyRef)).subscribe(value => {
      if (value.runtimeModel && (value.runtimeType !== 'openclaw' || value.launchType !== 'agent_runtime')) {
        this.automationForm.get('runtimeModel')?.setValue('', { emitEvent: false })
      }
    })
  }

  beforeUpload = (file: NzUploadFile): boolean => {
    this.selectedImage = file as unknown as File
    return false
  }

  handleImageUpload({ file }: NzUploadChangeParam): void {
    this.selectedImage = file.originFileObj
    const status = file.status
    if (status === 'done') {
      this.notification.create(
        'success',
        'Upload Successful',
        `${file.name} image uploaded successfully.`
      )
      //this.automationForm.get('image')?.setValue(file.response.imagePath);
    } else if (status === 'error') {
      this.notification.create(
        'error',
        'Upload Failed',
        `${file.name} image upload failed.`
      )
    }
  }

  onSubmit(): void {
    if (this.submitting) return
    if (!this.automationForm.valid) {
      for (const i in this.automationForm.controls) {
        this.automationForm.controls[i].markAsDirty()
        this.automationForm.controls[i].updateValueAndValidity()
      }
      return
    }

    const automation: IAutomationModel = {
      ...this.automationForm.value,
      runtimeModel: this.automationForm.value.runtimeType === 'openclaw' && this.automationForm.value.launchType === 'agent_runtime'
        ? this.automationForm.value.runtimeModel || '' : '',
      position: 0,
      imageFile: this.selectedImage,
      removeImage: this.removeImage,
    }

    if (!automation.id) {
      delete automation.id
    }

    this.submitting = true
    this.formDataSubmitted.emit(automation)
  }

  openModal(automation?: IAutomationModel, isUpdate: boolean = false) {
    this.isVisible = true
    this.isUpdate = isUpdate
    if (automation) {
      this.automationForm.patchValue({
        id: automation.id,
        name: automation.name,
        host: automation.host,
        port: automation.port,
        launchType: automation.launchType || 'browser_url',
        launchTarget: automation.launchTarget || '',
        runtimeType: automation.runtimeType || 'browser',
        runtimeModel: automation.runtimeModel || '',
        serviceName: automation.serviceName || '',
        routePath: automation.routePath || '',
        publicUrl: automation.publicUrl || '',
        localUrl: automation.localUrl || '',
        healthCheckType: automation.healthCheckType || 'http',
        healthCheckUrl: automation.healthCheckUrl || '',
        healthCheckIntervalSeconds: automation.healthCheckIntervalSeconds || 60,
        expectedHttpStatus: automation.expectedHttpStatus || 200,
        dependencyNotes: automation.dependencyNotes || '',
      })
      this.automationForm.get('image')?.setValue(automation.image)
    }
    if ((!this.modalTitle || this.modalTitle === '') && !this.isUpdate) {
      this.modalTitle = 'Add Automation'
    } else if ((!this.modalTitle || this.modalTitle === '') && this.isUpdate) {
      this.modalTitle = 'Update Automation'
    }
    this.changeDetector.markForCheck()
  }

  closeModal() {
    if (this.submitting) return
    this.resetModal()
  }

  completeSubmission(): void {
    this.submitting = false
    this.resetModal()
  }

  failSubmission(): void {
    this.submitting = false
    this.changeDetector.markForCheck()
  }

  private resetModal(): void {
    this.isVisible = false
    this.modalClosed.emit()
    this.automationForm.reset()
    this.selectedImage = undefined
    this.removeImage = false
    this.modalTitle = ''
    this.changeDetector.markForCheck()
  }

  deleteImage() {
    if (this.removeImage) {
      this.removeImage = false
    } else {
      this.removeImage = true
    }
  }
}
