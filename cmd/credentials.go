package cmd

import (
	"fmt"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// resolveEntry resolves the profile and returns the loaded keychain entry.
// When profile is empty, ResolveDefault is used.
// All errors include corrective guidance.
func resolveEntry(profile string) (keychain.Entry, error) {
	if profile == "" {
		var err error
		profile, err = keychain.ResolveDefault()
		if err != nil {
			return keychain.Entry{}, fmt.Errorf("no credentials — run: jiracli setup")
		}
	}
	entry, err := keychain.Load(profile)
	if err != nil {
		return keychain.Entry{}, fmt.Errorf("credentials not found for profile %q — run: jiracli setup", profile)
	}
	return entry, nil
}

// resolveEntryAndStore resolves the profile, loads the keychain entry, and
// constructs a cache.Store. Returns an error with a corrective message if the
// profile cannot be resolved or the entry cannot be loaded.
func resolveEntryAndStore(profile string) (keychain.Entry, *cache.Store, error) {
	entry, err := resolveEntry(profile)
	if err != nil {
		return keychain.Entry{}, nil, err
	}
	return entry, cache.NewStore(entry), nil
}
