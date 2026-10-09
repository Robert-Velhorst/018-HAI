import { ChangeDetectionStrategy, Component } from '@angular/core';
import { ThemeService } from './services/theme.service';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-root',
    templateUrl: './app.component.html',
    styleUrls: ['./app.component.scss'],
    standalone: false
})
export class AppComponent {
  title = 'app';

  constructor(private themeService: ThemeService) {}
}
