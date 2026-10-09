import { platformBrowserDynamic } from '@angular/platform-browser-dynamic';
import { provideZoneChangeDetection } from '@angular/core';

import { AppModule } from './app/app.module';


// Existing NgModule pages update subscription-backed fields. Keep their async
// render contract explicit instead of inheriting Angular's zoneless default.
platformBrowserDynamic().bootstrapModule(AppModule, {
  applicationProviders: [provideZoneChangeDetection({ eventCoalescing: true, runCoalescing: true })],
})
  .catch(err => console.error(err));
