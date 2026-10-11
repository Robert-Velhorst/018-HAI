import { provideZoneChangeDetection } from '@angular/core';
import { platformBrowser } from '@angular/platform-browser';

import { AppModule } from './app/app.module';


// HAI's existing modules update mutable view state from HttpClient and RxJS
// subscriptions. Angular 22 defaults to zoneless scheduling and OnPush
// components, so retain the official compatibility path while modules migrate
// incrementally to signals. Event coalescing avoids duplicate UI ticks. Do not
// enable run coalescing: it schedules HTTP-driven updates on animation frames,
// which browsers may suspend while this ambient control room is backgrounded.
platformBrowser().bootstrapModule(AppModule, {
  applicationProviders: [
    provideZoneChangeDetection({
      eventCoalescing: true,
    }),
  ],
})
  .catch(err => console.error(err));
