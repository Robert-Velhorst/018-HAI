import { ChangeDetectionStrategy, Component } from '@angular/core';
import { Router } from '@angular/router';

interface OnboardingStep {
  title: string;
  description: string;
}

const ONBOARDED_KEY = 'hai_onboarded';

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-onboarding',
  templateUrl: './onboarding.component.html',
  styleUrls: ['./onboarding.component.scss'],
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
        'Emergency stop halts execution instantly, and readiness tells you if the system is healthy.',
    },
  ];

  constructor(private router: Router) {}

  /** Whether onboarding has already been completed on this device. */
  static isOnboarded(): boolean {
    try {
      return localStorage.getItem(ONBOARDED_KEY) === 'true';
    } catch {
      return false;
    }
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
    try {
      localStorage.setItem(ONBOARDED_KEY, 'true');
    } catch {
      /* storage may be unavailable; proceed regardless */
    }
    this.router.navigate(['/control-center']);
  }

  skip(): void {
    this.finish();
  }
}
