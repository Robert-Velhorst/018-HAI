import {ErrorHandler, NgModule} from '@angular/core';
import {BrowserModule} from '@angular/platform-browser';
import {AppRoutingModule} from './app-routing.module';
import {AppComponent} from './app.component';
import {NZ_I18N} from 'ng-zorro-antd/i18n';
import {en_US} from 'ng-zorro-antd/i18n';
import {registerLocaleData} from '@angular/common';
import en from '@angular/common/locales/en';
import {provideHttpClient, withInterceptors, withXhr} from "@angular/common/http";
import {BrowserAnimationsModule} from '@angular/platform-browser/animations';
import {NZ_ICONS} from 'ng-zorro-antd/icon';
import {AUTH_SERVICE_TOKEN} from './services/auth/auth.service.token';
import {AuthService} from './services/auth/auth.service';
import {ChunkLoadRecoveryHandler} from './services/chunk-load-recovery.handler';
import {ControlRoomModule} from './control-room/control-room.module';
import {HAI_ICONS} from './hai-icons';
import {httpViewRefreshInterceptor} from './services/http-view-refresh.interceptor';

registerLocaleData(en);

export {HAI_ICONS} from './hai-icons';

@NgModule({
    declarations: [
        AppComponent,
    ],
    imports: [
        BrowserModule,
        AppRoutingModule,
        BrowserAnimationsModule,
        ControlRoomModule,
    ],
    providers: [
        {provide: NZ_I18N, useValue: en_US},
        {provide: NZ_ICONS, useValue: HAI_ICONS},
        {provide: AUTH_SERVICE_TOKEN, useClass: AuthService},
        {provide: ErrorHandler, useClass: ChunkLoadRecoveryHandler},
        provideHttpClient(withInterceptors([httpViewRefreshInterceptor]), withXhr()),
    ],
    bootstrap: [AppComponent]
})
export class AppModule {
}
