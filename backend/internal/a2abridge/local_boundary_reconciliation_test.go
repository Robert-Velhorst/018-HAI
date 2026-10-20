package a2abridge

import "testing"

func TestRetainedPrivateNetworkURLsDoNotBroadenBridgeBoundary(t *testing.T) {
	for _, endpoint := range []string{
		"http://10.0.0.1/api/v1/a2a",
		"http://172.16.0.1/api/v1/a2a",
		"http://192.168.0.1/api/v1/a2a",
		"http://[fd00::1]/api/v1/a2a",
		"http://127.0.0.1/another-path",
		"http://127.0.0.1/api/v1/%612a",
	} {
		t.Run(endpoint, func(t *testing.T) {
			planner := &previewStub{}
			service := NewService(Config{Enabled: true, Token: testBridgeToken, OwnerID: "owner", URL: endpoint}, planner)
			if service.Status().Configured {
				t.Fatal("retained URL rules must not broaden the current local bridge boundary")
			}
			if _, err := service.Draft("Prepare a draft"); err == nil {
				t.Fatal("an invalid bridge configuration must not plan work")
			}
			if len(planner.requests) != 0 {
				t.Fatal("invalid endpoint reached the planner")
			}
		})
	}
}
