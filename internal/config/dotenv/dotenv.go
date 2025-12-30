package dotenv

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// Load attempts to read environment variables from common .env locations.
//
// Notes:
//   - This is intended to improve local developer experience.
//   - In production (e.g. Kubernetes), env vars should be injected by the runtime.
//   - It never overrides variables already present in the process environment.
//
// Locations (loaded in order, if present):
//  1. $ENV_FILE (if set)
//  2. <cwd>/.env
//  3. <cwd>/internal/webhook/notify/.env
func Load() {
	candidates := make([]string, 0, 3)

	if envFile := os.Getenv("ENV_FILE"); envFile != "" {
		candidates = append(candidates, envFile)
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, ".env"),
			filepath.Join(wd, "internal", "webhook", "notify", ".env"),
		)
	} else {
		candidates = append(candidates,
			".env",
			filepath.Join("internal", "webhook", "notify", ".env"),
		)
	}

	seen := map[string]struct{}{}
	for _, f := range candidates {
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}

		if _, err := os.Stat(f); err != nil {
			continue
		}
		// Load() will not override existing process environment variables.
		_ = godotenv.Load(f)
	}
}
