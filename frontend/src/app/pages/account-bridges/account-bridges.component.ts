import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { forkJoin } from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  IBridgeContract,
  IFeedHealth,
} from '../../models/account-bridges.model.interface'
import { AccountBridgesService } from '../../services/account-bridges.service'

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-account-bridges',
  templateUrl: './account-bridges.component.html',
  styleUrls: ['./account-bridges.component.scss'],
})
export class AccountBridgesComponent implements OnInit {
  bridges: IBridgeContract[] = []
  feeds: IFeedHealth[] = []
  loading = false
  syncing: Record<string, boolean> = {}

  constructor(
    private service: AccountBridgesService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    this.loading = true
    forkJoin({
      bridges: this.service.bridges(),
      feeds: this.service.feeds(),
    }).subscribe({
      next: ({ bridges, feeds }) => {
        this.bridges = bridges.bridges ?? []
        this.feeds = feeds.feeds ?? []
        this.loading = false
      },
      error: () => {
        this.loading = false
        this.notification.error('Error', 'Failed to load account bridges.')
      },
    })
  }

  sync(f: IFeedHealth): void {
    this.syncing[f.feed.id] = true
    this.service.sync(f.feed.id).subscribe({
      next: (rep) => {
        this.syncing[f.feed.id] = false
        this.notification.success(
          'Synced',
          `${f.feed.name}: ${rep.itemsRead} read, ${rep.operationsCreated} new operations.`
        )
        this.refresh()
      },
      error: (err) => {
        this.syncing[f.feed.id] = false
        this.notification.error('Error', err?.error?.error ?? 'Sync failed.')
      },
    })
  }

  syncAll(): void {
    this.service.syncDue().subscribe({
      next: (res) => {
        const created = (res.reports ?? []).reduce((n, r) => n + r.operationsCreated, 0)
        this.notification.success('Synced all', `${created} new operations across ${res.reports?.length ?? 0} feeds.`)
        this.refresh()
      },
      error: () => this.notification.error('Error', 'Sync-all failed.'),
    })
  }

  statusColor(status: string): string {
    switch (status) {
      case 'available':
        return 'green'
      case 'credentials_present_unverified':
        return 'gold'
      case 'credentials_required':
        return 'blue'
      case 'contract_only':
        return 'default'
      default:
        return 'default'
    }
  }

  goBack(): void {
    this.router.navigate(['/control-center'])
  }
}
