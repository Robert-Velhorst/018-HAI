import { TestBed } from "@angular/core/testing";
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
});
