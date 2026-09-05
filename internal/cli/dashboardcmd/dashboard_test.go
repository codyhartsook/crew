package dashboardcmd

import (
	"context"
	"errors"
	"testing"
)

func TestOpenDashboard(t *testing.T) {
	for _, tc := range []struct {
		name, goos, url string
		appErr          error
		skip            bool
		wantApp         int
		wantBrowser     int
	}{
		{"mac app", "darwin", "http://127.0.0.1:8790", nil, false, 1, 0},
		{"browser fallback", "darwin", "http://localhost:8790", errors.New("not installed"), false, 1, 1},
		{"remote dashboard", "darwin", "https://crew.example.com", nil, false, 0, 1},
		{"other platform", "linux", "http://127.0.0.1:8790", nil, false, 0, 1},
		{"headless", "darwin", "http://127.0.0.1:8790", nil, true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var appCalls, browserCalls int
			app := func(context.Context, string, bool) error { appCalls++; return tc.appErr }
			browser := func(context.Context, string, bool) error { browserCalls++; return nil }
			if err := openDashboard(context.Background(), tc.url, tc.skip, tc.goos, app, browser); err != nil {
				t.Fatal(err)
			}
			if appCalls != tc.wantApp || browserCalls != tc.wantBrowser {
				t.Fatalf("calls = app %d, browser %d; want app %d, browser %d", appCalls, browserCalls, tc.wantApp, tc.wantBrowser)
			}
		})
	}
}
