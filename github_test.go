package githubloader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	aegis "github.com/goaegis/goaegis-core/aegis/core"
)

func TestGitHubAddon_LoadAndWatch(t *testing.T) {
	// 1. Thread-safe commit SHA mock control
	var commitSHA atomic.Value
	commitSHA.Store("sha_initial")

	var loadCount int32

	configContent := `
resources:
  posts:
    name: posts
roles:
  viewer:
    name: viewer
    permissions:
      - resource: posts
        actions: [read]
        effect: allow
subjects:
  user:alice:
    id: user:alice
    roles: [viewer]
`

	// 2. Setup local mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/commits") {
			sha := commitSHA.Load().(string)
			commits := []GitHubCommit{{SHA: sha}}
			_ = json.NewEncoder(w).Encode(commits)
			return
		}

		if strings.Contains(r.URL.Path, "/contents/auth/config.yaml") {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(configContent))
			return
		}

		if strings.Contains(r.URL.Path, "/contents/auth") {
			atomic.AddInt32(&loadCount, 1)
			downloadURL := "http://" + r.Host + "/contents/auth/config.yaml"
			contents := []GitHubContent{
				{
					Name:        "config.yaml",
					Path:        "auth/config.yaml",
					Type:        "file",
					DownloadURL: downloadURL,
				},
			}
			_ = json.NewEncoder(w).Encode(contents)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// 3. Create addon and set local server base URL
	addon := New("goaegis", "test-repo", "auth", "main", "dummy_token", 20*time.Millisecond)
	addon.apiBaseURL = server.URL // Point to mock HTTP test server

	// 4. Test Aegis integration
	authz := aegis.New()
	defer authz.Shutdown()

	err := authz.Use(addon)
	if err != nil {
		t.Fatalf("Failed to register github addon: %v", err)
	}

	// Load config from addon
	err = authz.LoadConfigFromAddon()
	if err != nil {
		t.Fatalf("Failed to load config from github addon: %v", err)
	}

	// Validate authorization decision
	allowed, err := authz.Can("user:alice", "posts", "read", nil)
	if err != nil {
		t.Fatalf("Can() failed: %v", err)
	}
	if !allowed {
		t.Errorf("expected allowed, got denied")
	}

	// 5. Test Hot Reload via commit polling
	initialLoads := atomic.LoadInt32(&loadCount)
	commitSHA.Store("sha_new") // Update SHA

	// Wait for background polling to detect change and call ReloadConfig
	time.Sleep(100 * time.Millisecond)

	finalLoads := atomic.LoadInt32(&loadCount)
	if finalLoads <= initialLoads {
		t.Errorf("Expected configuration to reload (load count to increase), but got loads initial=%d, final=%d", initialLoads, finalLoads)
	}
}
