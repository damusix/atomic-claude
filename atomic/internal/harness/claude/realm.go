package claude

import "github.com/damusix/atomic-claude/atomic/internal/hooks"

// RealmSettingsPath is the realm-relative Claude settings file for local hooks.
const RealmSettingsPath = ".claude/settings.local.json"

// RealmSessionStart projects the owned startup member into or out of local settings.
func RealmSessionStart(settings []byte, ownedDigest string, enable bool) (next []byte, memberDigest string, err error) {
	return hooks.ProjectRealmSessionStart(settings, ownedDigest, enable)
}

// RealmSessionStartOwned checks for exactly one member matching the ownership receipt.
func RealmSessionStartOwned(settings []byte, ownedDigest string) bool {
	return hooks.RealmSessionStartOwned(settings, ownedDigest)
}
