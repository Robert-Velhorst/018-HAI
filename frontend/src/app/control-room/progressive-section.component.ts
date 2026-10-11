import { ChangeDetectionStrategy, Component, EventEmitter, Input, OnChanges, Output, SimpleChanges } from '@angular/core'
import { ModuleViewPreferencesService } from './module-view-preferences.service'

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  standalone: false,
  selector: 'hai-progressive-section',
  templateUrl: './progressive-section.component.html',
})
export class HaiProgressiveSectionComponent implements OnChanges {
  @Input() moduleId = ''
  @Input() sectionId = ''
  @Input() title = ''
  @Input() summary = ''
  @Input() advancedOnly = true
  @Output() openChange = new EventEmitter<boolean>()
  open = false

  constructor(private preferences: ModuleViewPreferencesService) {}

  ngOnChanges(_: SimpleChanges): void {
    if (this.moduleId && this.sectionId) this.open = this.preferences.get(this.moduleId).openSections[this.sectionId] === true
  }

  toggle(): void {
    this.setOpen(!this.open)
  }

  setOpen(open: boolean): void {
    if (this.open === open) return
    this.open = open
    this.preferences.setSection(this.moduleId, this.sectionId, open)
    this.openChange.emit(open)
  }
}
