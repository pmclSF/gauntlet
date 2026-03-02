package fixture

import "fmt"

// MigrateFixtures recomputes hashes for all fixtures when the hash version changes.
// In v1, this is a stub — hash_version is always 1.
func MigrateFixtures(store *Store, fromVersion, toVersion int) error {
	if fromVersion == toVersion {
		return nil
	}
	return fmt.Errorf("fixture migration from version %d to %d is not yet supported (v2 feature)", fromVersion, toVersion)
}
