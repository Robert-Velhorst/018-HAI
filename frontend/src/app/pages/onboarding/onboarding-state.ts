const ONBOARDED_KEY = 'hai_onboarded';
let completedForSession = false;

export function isOnboardingComplete(): boolean {
  try {
    return localStorage.getItem(ONBOARDED_KEY) === 'true' || completedForSession;
  } catch {
    return completedForSession;
  }
}

export function completeOnboarding(): void {
  try {
    localStorage.setItem(ONBOARDED_KEY, 'true');
  } catch {
    completedForSession = true;
  }
}
