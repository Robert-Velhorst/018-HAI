import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { IHAIOSOverview } from '../../models/hai-os.model.interface';
import { HAI_OS_SERVICE_TOKEN } from '../../services/hai-os/hai-os.service.token';
import { IHAIOSService } from '../../services/hai-os.service.interface';

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-hai-os',
  templateUrl: './hai-os.component.html',
  styleUrls: ['./hai-os.component.scss'],
})
export class HAIOSComponent implements OnInit {
  overview?: IHAIOSOverview;
  loading = false;

  constructor(
    @Inject(HAI_OS_SERVICE_TOKEN) private haiOSService: IHAIOSService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh();
  }

  refresh(): void {
    this.loading = true;
    this.haiOSService.overview().subscribe({
      next: (overview) => {
        this.overview = overview;
        this.loading = false;
      },
      error: () => {
        this.loading = false;
        this.notification.error('Error', 'Failed to load HAI OS overview.');
      },
    });
  }

  open(link: string): void {
    this.router.navigate([link]);
  }

  openPursuits(selected?: string): void {
    this.router.navigate(['/pursuits'], {
      queryParams: selected ? { selected } : undefined,
    });
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }
}
