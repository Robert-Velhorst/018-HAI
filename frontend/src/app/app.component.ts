import { Component } from '@angular/core';
import { ThemeService } from './services/theme.service';

@Component({
  standalone: false,
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss']
})
export class AppComponent {
  readonly title = 'HAI Automation Hub';

  constructor(private themeService: ThemeService) {}
}
