import { fakeAsync, flushMicrotasks, TestBed, tick } from "@angular/core/testing";
import { DOCUMENT } from "@angular/common";
import { provideHttpClient } from "@angular/common/http";
import {
  HttpTestingController,
  provideHttpClientTesting,
} from "@angular/common/http/testing";
import { AuthenticatedThemeService } from "./authenticated-theme.service";

describe("AuthenticatedThemeService", () => {
  let service: AuthenticatedThemeService;
  let http: HttpTestingController;
  let document: Document;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(AuthenticatedThemeService);
    http = TestBed.inject(HttpTestingController);
    document = TestBed.inject(DOCUMENT);
  });

  afterEach(() => {
    http.verify();
    document.querySelector("link[data-hai-authenticated-theme]")?.remove();
  });

  it("loads the hashed stylesheet from the build manifest once", async () => {
    let stylesheetLink: HTMLLinkElement | undefined;
    spyOn(document.head, "append").and.callFake((...nodes: (Node | string)[]) => {
      stylesheetLink = nodes[0] as HTMLLinkElement;
    });
    const loaded = service.load();
    const request = http.expectOne(
      new URL("authenticated-theme-manifest.json", document.baseURI).toString(),
    );
    request.flush({ href: "authenticated-theme-abc123.css" });
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(stylesheetLink?.href).toBe(
      new URL("authenticated-theme-abc123.css", document.baseURI).toString(),
    );
    stylesheetLink?.dispatchEvent(new Event("load"));

    await expectAsync(loaded).toBeResolvedTo(true);
    await expectAsync(service.load()).toBeResolvedTo(true);
    http.expectNone(
      new URL("authenticated-theme-manifest.json", document.baseURI).toString(),
    );
  });

  it("rejects a stylesheet URL that points off-origin", async () => {
    const loaded = service.load();
    http.expectOne(
      new URL("authenticated-theme-manifest.json", document.baseURI).toString(),
    ).flush({ href: "https://untrusted.invalid/theme.css" });

    await expectAsync(loaded).toBeResolvedTo(false);
    expect(document.querySelector("link[data-hai-authenticated-theme]")).toBeNull();
  });

  it("bounds a stalled stylesheet, removes it, ignores late load, and permits retry", fakeAsync(() => {
    let outcome: boolean | undefined;
    void service.load().then(value => { outcome = value; });
    const manifestUrl = new URL("authenticated-theme-manifest.json", document.baseURI).toString();
    http.expectOne(manifestUrl).flush({ href: "authenticated-theme-stalled.css" });
    flushMicrotasks();
    const link = document.querySelector<HTMLLinkElement>("link[data-hai-authenticated-theme]")!;
    expect(link).not.toBeNull();
    const lateLoad = link.onload;
    tick(4999);
    expect(outcome).toBeUndefined();
    tick(1);
    flushMicrotasks();
    expect(outcome).toBeFalse();
    expect(document.querySelector("link[data-hai-authenticated-theme]")).toBeNull();
    lateLoad?.call(link, new Event("load"));

    let retry: boolean | undefined;
    void service.load().then(value => { retry = value; });
    http.expectOne(manifestUrl).flush({ href: "authenticated-theme-retry.css" });
    flushMicrotasks();
    const retryLink = document.querySelector<HTMLLinkElement>("link[data-hai-authenticated-theme]")!;
    retryLink.dispatchEvent(new Event("load"));
    flushMicrotasks();
    expect(retry).toBeTrue();
  }));
});
