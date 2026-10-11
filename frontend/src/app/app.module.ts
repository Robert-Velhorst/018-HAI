import {ErrorHandler, NgModule} from '@angular/core';
import {BrowserModule} from '@angular/platform-browser';
import {AppRoutingModule} from './app-routing.module';
import {AppComponent} from './app.component';
import {NZ_I18N} from 'ng-zorro-antd/i18n';
import {en_US} from 'ng-zorro-antd/i18n';
import {registerLocaleData} from '@angular/common';
import en from '@angular/common/locales/en';
import {HTTP_INTERCEPTORS, provideHttpClient, withInterceptorsFromDi} from '@angular/common/http';
import {provideAnimationsAsync} from '@angular/platform-browser/animations/async';
import {BulbOutline, CalendarOutline, ContactsOutline, DisconnectOutline, HeartOutline, NodeIndexOutline, ReloadOutline, StarOutline, WarningFill, WarningOutline} from '@ant-design/icons-angular/icons';
import {NZ_ICONS} from 'ng-zorro-antd/icon';
import {AUTH_SERVICE_TOKEN} from './services/auth/auth.service.token';
import {AuthService} from './services/auth/auth.service';
import {ChunkLoadRecoveryHandler} from './services/chunk-load-recovery.handler';
import {RequestTimeoutInterceptor} from './interceptors/request-timeout.interceptor';

registerLocaleData(en);

export const HAI_ICONS = [
    BulbOutline,
    CalendarOutline,
    ContactsOutline,
    DisconnectOutline,
    HeartOutline,
    NodeIndexOutline,
    ReloadOutline,
    StarOutline,
    WarningFill,
    WarningOutline,
];

@NgModule({
    declarations: [
        AppComponent,
    ],
    imports: [
        BrowserModule,
        AppRoutingModule,
    ],
    providers: [
        {provide: NZ_I18N, useValue: en_US},
        {provide: NZ_ICONS, useValue: HAI_ICONS},
        {provide: AUTH_SERVICE_TOKEN, useClass: AuthService},
        {provide: ErrorHandler, useClass: ChunkLoadRecoveryHandler},
        {provide: HTTP_INTERCEPTORS, useClass: RequestTimeoutInterceptor, multi: true},
        provideAnimationsAsync(),
        provideHttpClient(withInterceptorsFromDi()),
    ],
    bootstrap: [AppComponent],
})
export class AppModule {
}
