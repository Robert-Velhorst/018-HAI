package providerfallback

import "testing"

func TestPrefersFirstAvailableFreeProvider(t *testing.T) {
	providers := []Provider{
		{Name: "ollama-down", Available: false, Paid: false},
		{Name: "ollama-up", Available: true, Paid: false},
		{Name: "openai", Available: true, Paid: true},
	}
	got, ok := Select(providers, true)
	if !ok || got.Name != "ollama-up" {
		t.Fatalf("selected %q (ok=%v), want ollama-up", got.Name, ok)
	}
}

func TestNeverSelectsPaidWhenDisallowed(t *testing.T) {
	providers := []Provider{
		{Name: "local", Available: false, Paid: false},
		{Name: "openai", Available: true, Paid: true},
	}
	if _, ok := Select(providers, false); ok {
		t.Fatalf("paid provider selected while paid usage disallowed")
	}
}

func TestFallsBackToPaidOnlyWhenAllowed(t *testing.T) {
	providers := []Provider{
		{Name: "local", Available: false, Paid: false},
		{Name: "openai", Available: true, Paid: true},
	}
	got, ok := Select(providers, true)
	if !ok || got.Name != "openai" {
		t.Fatalf("selected %q (ok=%v), want openai fallback", got.Name, ok)
	}
}

func TestSkipsAvailableProvidersWithoutIdentity(t *testing.T) {
	t.Run("falls through to a named free provider", func(t *testing.T) {
		providers := []Provider{
			{Name: " \t ", Available: true, Paid: false},
			{Name: "ollama", Available: true, Paid: false},
		}
		got, ok := Select(providers, true)
		if !ok || got.Name != "ollama" {
			t.Fatalf("selected %#v (ok=%v), want named free provider ollama", got, ok)
		}
	})

	t.Run("does not treat an unnamed free provider as a route", func(t *testing.T) {
		providers := []Provider{{Available: true, Paid: false}}
		if got, ok := Select(providers, false); ok {
			t.Fatalf("selected %#v without a provider identity", got)
		}
	})

	t.Run("does not select an unnamed paid provider even when paid use is allowed", func(t *testing.T) {
		providers := []Provider{{Available: true, Paid: true}}
		if got, ok := Select(providers, true); ok {
			t.Fatalf("selected %#v without a provider identity", got)
		}
	})

	t.Run("unnamed free provider cannot mask permitted named paid fallback", func(t *testing.T) {
		providers := []Provider{
			{Name: "", Available: true, Paid: false},
			{Name: "openai", Available: true, Paid: true},
		}
		got, ok := Select(providers, true)
		if !ok || got.Name != "openai" {
			t.Fatalf("selected %#v (ok=%v), want explicitly permitted paid fallback", got, ok)
		}
	})
}

func TestDeterministicAndEmpty(t *testing.T) {
	if _, ok := Select(nil, true); ok {
		t.Fatalf("empty provider list should select nothing")
	}
	providers := []Provider{{Name: "a", Available: true}, {Name: "b", Available: true}}
	for i := 0; i < 50; i++ {
		got, _ := Select(providers, true)
		if got.Name != "a" {
			t.Fatalf("selection not deterministic: got %q", got.Name)
		}
	}
}
