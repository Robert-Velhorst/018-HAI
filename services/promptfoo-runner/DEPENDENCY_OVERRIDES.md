# MSAL security integration

The 24 October default-branch audit identified MSAL Node authorization-code
origin-validation advisory [GHSA-xcx6-jx8p-x3h5](https://github.com/advisories/GHSA-xcx6-jx8p-x3h5).
The advisory affects versions below 7.1.0 from the major-5 beta onward.

Azure Identity 4.14.0 is the checked current public stable release, but its
manifest still requests MSAL Node `^6.0.0`. Updating the top-level MSAL dependency
therefore leaves a vulnerable nested copy. This runner explicitly overrides
Azure Identity's MSAL dependency to 7.1.1. That is a major-version integration
outside the parent's declared range; the override does not mean Azure Identity
has declared major-7 support.

The upstream MSAL 7.0.0 release identifies removal of `responseMode` from
`acquireTokenInteractive` and the requirement for `form_post` in browser-loopback
authentication. Static review of the integrity-verified Azure Identity 4.14.0
package finds its interactive calls using MSAL's default loopback behavior and
not supplying `responseMode`. That review supports preparing the integration;
it is not proof of successful real Azure authentication.

Require the existing Node 26.12.1 Promptfoo contract/import checks, the complete
lock audit and the rest of current-head CI. Do not use a zero-advisory result as
evidence of live credential/provider acceptance. No Azure account, token flow,
provider connection or deployment is operated by this dependency change.

Sources:

- [MSAL Node 7.0.0 release](https://github.com/AzureAD/microsoft-authentication-library-for-js/releases/tag/msal-node-v7.0.0)
- [MSAL Node 7.1.1 release](https://github.com/AzureAD/microsoft-authentication-library-for-js/releases/tag/msal-node-v7.1.1)
- [Azure Identity 4.14.0 manifest](https://registry.npmjs.org/@azure%2fidentity/4.14.0)

Other transitive major migrations remain separate from this security stage.
The existing get-uri/basic-ftp override and all scan gates remain intact.
