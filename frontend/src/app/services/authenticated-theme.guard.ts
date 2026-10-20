import { inject } from "@angular/core";
import { CanActivateChildFn } from "@angular/router";
import { AuthenticatedThemeService } from "./authenticated-theme.service";

export const authenticatedThemeGuard: CanActivateChildFn = async () => {
  const loaded = await inject(AuthenticatedThemeService).load();
  if (!loaded) console.warn("Authenticated HAI styles could not be loaded.");
  return true;
};
