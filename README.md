# goaegis-github

**Remote GitHub configuration loader addon for the `goaegis` authorization framework.**

Allows `goaegis` to fetch and load authorization configurations directly from a GitHub repository (public or private), support recursive directory fetches (multi-file layouts), and periodically poll commit history to trigger hot reloads.

## 🚀 Installation

```bash
go get github.com/goaegis/goaegis-github
```

## 🛠️ Usage

```go
package main

import (
	"log"
	"time"

	aegis "github.com/goaegis/goaegis-core/aegis/core"
	githubloader "github.com/goaegis/goaegis-github"
)

func main() {
	authz := aegis.New()
	defer authz.Shutdown()

	// Initialize GitHub Addon
	// owner, repo, path, branch, token, pollInterval
	addon := githubloader.New(
		"goaegis",
		"auth-config-repo",
		"configs",
		"main",
		"YOUR_GITHUB_PERSONAL_ACCESS_TOKEN",
		15*time.Second,
	)

	// Register addon
	if err := authz.Use(addon); err != nil {
		log.Fatal(err)
	}

	// Load configuration from GitHub
	if err := authz.LoadConfigFromAddon(); err != nil {
		log.Fatal(err)
	}

	log.Println("goaegis configuration loaded successfully from GitHub!")
}
```
