package main

import (
	"log"
	"time"

	aegis "github.com/goaegis/goaegis-core/aegis/core"
	githubloader "github.com/goaegis/goaegis-github"
)

func main() {
	log.Println("=== goaegis GitHub Loader Example ===")

	authz := aegis.New()
	defer authz.Shutdown()

	// Initialize the GitHub addon to load files under the "auth" directory
	// from repository github.com/goaegis/configs on branch "main"
	// with a polling interval of 10 seconds.
	addon := githubloader.New(
		"goaegis",
		"configs",
		"auth",
		"main",
		"", // Leave empty for public repos, or provide Personal Access Token (PAT)
		10*time.Second,
	)

	// Register the addon
	if err := authz.Use(addon); err != nil {
		log.Fatalf("failed to register github addon: %v", err)
	}

	// Load config from the GitHub addon
	log.Println("Loading config from GitHub remote repository...")
	if err := authz.LoadConfigFromAddon(); err != nil {
		log.Fatalf("failed to load remote config: %v", err)
	}

	log.Println("Config loaded successfully! Listening for remote changes...")
}
