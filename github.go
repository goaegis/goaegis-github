package githubloader

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/goaegis/goaegis-core/aegis/addons"
	"github.com/goaegis/goaegis-core/aegis/config"
)

type GitHubAddon struct {
	owner        string
	repo         string
	path         string
	branch       string
	token        string
	pollInterval time.Duration
	watchCh      chan struct{}
	stopCh       chan struct{}
	client       *http.Client
	lastCommit   string
	apiBaseURL   string // Added for testability
}

type GitHubContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	DownloadURL string `json:"download_url"`
}

type GitHubCommit struct {
	SHA string `json:"sha"`
}

func New(owner, repo, path, branch, token string, pollInterval time.Duration) *GitHubAddon {
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}
	return &GitHubAddon{
		owner:        owner,
		repo:         repo,
		path:         path,
		branch:       branch,
		token:        token,
		pollInterval: pollInterval,
		client:       &http.Client{Timeout: 10 * time.Second},
		apiBaseURL:   "https://api.github.com",
	}
}

func (g *GitHubAddon) Name() string {
	return "github-config-loader"
}

func (g *GitHubAddon) Init(core any) error {
	g.watchCh = make(chan struct{}, 1)
	g.stopCh = make(chan struct{})
	log.Printf("[github-loader] Initialized (owner: %s, repo: %s, path: %s, branch: %s)", g.owner, g.repo, g.path, g.branch)
	return nil
}

func (g *GitHubAddon) OnBeforeConfigLoad(path string) (addons.ConfigSource, error) {
	log.Printf("[github-loader] Providing GitHub config source")
	
	// Fetch initial commit SHA
	sha, err := g.fetchLatestCommitSHA()
	if err == nil {
		g.lastCommit = sha
	} else {
		log.Printf("[github-loader] Warning: failed to fetch initial commit SHA: %v", err)
	}

	// Start polling in background
	go g.pollForChanges()

	return g, nil
}

func (g *GitHubAddon) LoadFiles() (map[string][]byte, error) {
	log.Printf("[github-loader] Loading config from github.com/%s/%s/%s (branch: %s)", g.owner, g.repo, g.path, g.branch)

	filesMap := make(map[string][]byte)
	err := g.fetchContentsRecursive(g.path, filesMap)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch contents from GitHub: %w", err)
	}

	if len(filesMap) == 0 {
		return nil, fmt.Errorf("no config files found in github.com/%s/%s/%s", g.owner, g.repo, g.path)
	}

	log.Printf("[github-loader] Loaded %d config files successfully", len(filesMap))
	return filesMap, nil
}

func (g *GitHubAddon) Watch() <-chan struct{} {
	return g.watchCh
}

func (g *GitHubAddon) OnConfigValidate(cfg *config.Config) (*config.Config, error) {
	return cfg, nil
}

func (g *GitHubAddon) OnConfigLoad(cfg *config.Config) error {
	log.Println("[github-loader] Config successfully loaded into Core")
	return nil
}

func (g *GitHubAddon) OnAuthorize(ctx *addons.Context) (addons.Decision, error) {
	return addons.Abstain, nil
}

func (g *GitHubAddon) Shutdown() error {
	log.Println("[github-loader] Shutting down")
	close(g.stopCh)
	return nil
}

func (g *GitHubAddon) fetchContentsRecursive(currentPath string, filesMap map[string][]byte) error {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", g.apiBaseURL, g.owner, g.repo, currentPath)
	if g.branch != "" {
		url += "?ref=" + g.branch
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	g.addHeaders(req)

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Response can be a single file object or a list of contents
	var contents []GitHubContent
	if err := json.Unmarshal(bodyBytes, &contents); err != nil {
		// Try parsing as single content item
		var singleContent GitHubContent
		if err2 := json.Unmarshal(bodyBytes, &singleContent); err2 != nil {
			return fmt.Errorf("failed to parse GitHub response: %v (fallback error: %v)", err, err2)
		}
		contents = []GitHubContent{singleContent}
	}

	for _, item := range contents {
		if item.Type == "dir" {
			// Recurse down the directory
			err := g.fetchContentsRecursive(item.Path, filesMap)
			if err != nil {
				return err
			}
		} else if item.Type == "file" {
			// Check if file is a YAML or aegis file
			if strings.HasSuffix(item.Name, ".yaml") || strings.HasSuffix(item.Name, ".yml") || strings.HasSuffix(item.Name, ".aegis") {
				contentBytes, err := g.downloadRawFile(item.DownloadURL)
				if err != nil {
					return fmt.Errorf("failed to download file %s: %w", item.Name, err)
				}
				filesMap[item.Path] = contentBytes
			}
		}
	}

	return nil
}

func (g *GitHubAddon) downloadRawFile(downloadURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}

	g.addHeaders(req)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (g *GitHubAddon) fetchLatestCommitSHA() (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits?path=%s&per_page=1", g.apiBaseURL, g.owner, g.repo, g.path)
	if g.branch != "" {
		url += "&sha=" + g.branch
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	g.addHeaders(req)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var commits []GitHubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return "", err
	}

	if len(commits) == 0 {
		return "", fmt.Errorf("no commits found for path %s", g.path)
	}

	return commits[0].SHA, nil
}

func (g *GitHubAddon) pollForChanges() {
	ticker := time.NewTicker(g.pollInterval)
	defer ticker.Stop()

	log.Printf("[github-loader] Started polling for changes every %v", g.pollInterval)

	for {
		select {
		case <-ticker.C:
			sha, err := g.fetchLatestCommitSHA()
			if err != nil {
				log.Printf("[github-loader] Polling error: %v", err)
				continue
			}

			if g.lastCommit == "" {
				g.lastCommit = sha
				continue
			}

			if sha != g.lastCommit {
				log.Printf("[github-loader] Commit changed from %s to %s. Triggering reload.", g.lastCommit, sha)
				g.lastCommit = sha
				select {
				case g.watchCh <- struct{}{}:
				default:
				}
			}
		case <-g.stopCh:
			log.Println("[github-loader] Stopped polling")
			return
		}
	}
}

func (g *GitHubAddon) addHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "goaegis-github-addon")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if g.token != "" {
		req.Header.Set("Authorization", "token "+g.token)
	}
}
