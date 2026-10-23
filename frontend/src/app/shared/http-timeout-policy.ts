const sourceTranscriptionPath = /^\/api\/v1\/sources\/[^/]+\/transcribe$/;
const sourceDocumentExtractionPath = /^\/api\/v1\/sources\/[^/]+\/extract-documents$/;
const hireAIJobsSearchPath = /^\/api\/v1\/hire-ai\/jobs\/search$/;
const reviewedArchiveUploadPath = /^\/api\/v1\/agent-runtimes\/openclaw\/ecosystem\/(?:approval\/)?upload$/;
const configuredSourceRunnerMaximumMs = 10 * 60_000;

function normalizedPath(requestUrl: string): string {
  let pathname: string;
  try {
    pathname = new URL(requestUrl, 'http://hai.invalid').pathname;
  } catch {
    pathname = requestUrl.split(/[?#]/, 1)[0];
  }
  return pathname.replace(/\\/g, '/').replace(/\/{2,}/g, '/').replace(/\/+$/, '') || '/';
}

export const HttpTimeoutPolicy = Object.freeze({
  readMs: 8_000,
  operationMs: 30_000,
  // HAI permits a 30-second upstream timeout; leave bounded response overhead.
  hireAISearchMs: 35_000,
  reviewedArchiveUploadMs: 15 * 60_000,
  // Both local runners accept up to 600 seconds; the gateway allows 15 minutes.
  sourceTranscriptionMs: configuredSourceRunnerMaximumMs,
  sourceDocumentExtractionMs: configuredSourceRunnerMaximumMs,

  forRequest(method: string, requestUrl: string): number {
    if (method.toUpperCase() === 'GET') return this.readMs;
    if (method.toUpperCase() !== 'POST') return this.operationMs;

    const path = normalizedPath(requestUrl);
    if (reviewedArchiveUploadPath.test(path)) return this.reviewedArchiveUploadMs;
    if (hireAIJobsSearchPath.test(path)) return this.hireAISearchMs;
    if (sourceTranscriptionPath.test(path)) return this.sourceTranscriptionMs;
    if (sourceDocumentExtractionPath.test(path)) return this.sourceDocumentExtractionMs;
    return this.operationMs;
  },
});
