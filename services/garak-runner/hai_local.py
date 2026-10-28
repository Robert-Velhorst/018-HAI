"""Owned Garak extension: authenticated local traffic never follows redirects.

Uses Garak0.17.1's documented OpenAICompatible._load_unsafe extension point.
This additive module changes no original publisher file or SDK test.
"""
import openai
from garak.generators.openai import OpenAICompatible


class LocalOpenAICompatible(OpenAICompatible):
    """Keep the original generation protocol, but fail closed on model errors."""

    def _load_unsafe(self):
        if self.name in ("", None):
            raise ValueError("LocalOpenAICompatible requires a model name")
        transport = openai.DefaultHttpxClient(follow_redirects=False)
        try:
            client = openai.OpenAI(base_url=self.uri, api_key=self.api_key, http_client=transport)
        except BaseException:
            transport.close()
            raise
        self.client = client
        self.generator = client.chat.completions

    def _call_model(self, *args, **kwargs):
        results = super()._call_model(*args, **kwargs)
        # Upstream converts nontransient HTTP errors into [None]. Such a model
        # failure must not be scored as a successful fixed safety evaluation.
        if not results or any(result is None or getattr(result, "text", None) is None for result in results):
            raise RuntimeError("configured local model did not return a completion")
        return results


DEFAULT_CLASS = "LocalOpenAICompatible"
