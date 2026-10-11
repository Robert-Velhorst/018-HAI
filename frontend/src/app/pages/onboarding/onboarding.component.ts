import { ChangeDetectionStrategy, Component } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { safePostOnboardingDestination } from '../../services/auth/guards/safe-return-url';
import { completeOnboarding, isOnboardingComplete } from './onboarding-state';

interface OnboardingStep {
  title: string;
  description: string;
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-onboarding',
    templateUrl: './onboarding.component.html',
    styleUrls: ['./onboarding.component.scss'],
    standalone: false
})
export class OnboardingComponent {
  current = 0;

  readonly steps: OnboardingStep[] = [
    {
      title: 'Welcome',
      description:
        'HAI is your local-first Personal AI Operating System. Your data stays on your machine.',
    },
    {
      title: 'Remember',
      description:
        'Capture context as memories. HAI dedupes and retrieves what is relevant when you need it.',
    },
    {
      title: 'Approve',
      description:
        'Nothing with real side effects runs without your approval. You are always in control.',
    },
    {
      title: 'Stay safe',
      description:
        'Emergency stop blocks new execution immediately and reports whether an in-flight runtime supports cancellation. Readiness tells you if the system is healthy.',
    },
  ];

  constructor(private router: Router, private route: ActivatedRoute) {}

  /** Whether onboarding has already been completed on this device. */
  static isOnboarded(): boolean {
    return isOnboardingComplete();
  }

  get isLastStep(): boolean {
    return this.current === this.steps.length - 1;
  }

  next(): void {
    if (this.current < this.steps.length - 1) {
      this.current += 1;
    }
  }

  prev(): void {
    if (this.current > 0) {
      this.current -= 1;
    }
  }

  finish(): void {
    completeOnboarding();
    const destination = safePostOnboardingDestination(
      this.route.snapshot.queryParamMap.get('returnUrl'),
    );
    void this.router.navigateByUrl(destination ?? '/control-center');
  }

  skip(): void {
    this.finish();
  }
}
