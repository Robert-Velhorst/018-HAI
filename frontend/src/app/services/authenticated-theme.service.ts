import { DOCUMENT } from "@angular/common";
import { HttpClient } from "@angular/common/http";
import { Inject, Injectable } from "@angular/core";
import { firstValueFrom, timeout } from "rxjs";

interface ThemeManifest {
  href: string;
}

@Injectable({ providedIn: "root" })
export class AuthenticatedThemeService {
  private loading?: Promise<boolean>;
  private loaded = false;

  constructor(
    private readonly http: HttpClient,
    @Inject(DOCUMENT) private readonly document: Document,
  ) {}

  load(): Promise<boolean> {
    if (this.loaded) return Promise.resolve(true);
    if (this.loading) return this.loading;

    const manifestUrl = new URL(
      "authenticated-theme-manifest.json",
      this.document.baseURI,
    ).toString();
    this.loading = firstValueFrom(
      this.http.get<ThemeManifest>(manifestUrl).pipe(timeout(5000)),
    )
      .then(({ href }) => this.attachStylesheet(href))
      .catch(() => this.attachStylesheet("authenticated-theme.css"))
      .finally(() => {
        if (!this.loaded) {
          this.loading = undefined;
        }
      });

    return this.loading;
  }

  private attachStylesheet(href: string): Promise<boolean> {
    if (!href || href.includes("..") || href.startsWith("//")) return Promise.resolve(false);
    const url = new URL(href, this.document.baseURI);
    if (url.origin !== new URL(this.document.baseURI).origin) return Promise.resolve(false);

    const existing = this.document.querySelector<HTMLLinkElement>(
      "link[data-hai-authenticated-theme]",
    );
    if (existing?.getAttribute("data-loaded") === "true") {
      this.loaded = true;
      return Promise.resolve(true);
    }
    existing?.remove();

    return new Promise((resolve) => {
      const link = this.document.createElement("link");
      link.rel = "stylesheet";
      link.href = url.toString();
      link.setAttribute("data-hai-authenticated-theme", "true");
      let settled = false;
      const finish = (success: boolean) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        link.onload = null;
        link.onerror = null;
        if (success) {
          this.loaded = true;
          link.setAttribute("data-loaded", "true");
        } else {
          link.remove();
        }
        resolve(success);
      };
      const timer = setTimeout(() => finish(false), 5000);
      link.onload = () => finish(true);
      link.onerror = () => finish(false);
      this.document.head.append(link);
    });
  }
}
