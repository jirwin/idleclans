package quests

import "strings"

// BossToKey maps boss names to their required key types
var BossToKey = map[string]string{
	"griffin": "mountain",
	"medusa":  "stone",
	"hades":   "underworld",
	"zeus":    "godly",
	"devil":   "burning",
	"chimera": "mutated",
	"dragon":  "otherworldly",
	"sobek":   "ancient",
	"kronos":  "kronos",
}

// KeyToColor maps key types to their colors (for user-friendly input)
var KeyToColor = map[string]string{
	"mountain":     "brown",
	"stone":        "gray",
	"underworld":   "blue",
	"godly":        "gold",
	"burning":      "red",
	"mutated":      "green",
	"otherworldly": "otherworldly",
	"ancient":      "ancient",
	"kronos":       "book",
}

// ColorToKey maps colors to key types
var ColorToKey = make(map[string]string)

func init() {
	// Build reverse mapping
	for key, color := range KeyToColor {
		ColorToKey[strings.ToLower(color)] = key
	}
}

// ValidBosses returns a list of all valid boss names
func ValidBosses() []string {
	bosses := make([]string, 0, len(BossToKey))
	for boss := range BossToKey {
		bosses = append(bosses, boss)
	}
	return bosses
}

// IsValidBoss checks if a boss name is valid
func IsValidBoss(boss string) bool {
	_, ok := BossToKey[boss]
	return ok
}

// GetKeyForBoss returns the key type required for a boss
func GetKeyForBoss(boss string) (string, bool) {
	key, ok := BossToKey[boss]
	return key, ok
}

// ResolveBossName attempts to resolve a boss name from various input formats:
// 1. Full boss name (e.g., "griffin")
// 2. Single letter (first letter of boss name, e.g., "g" for "griffin")
// 3. Key color (e.g., "red" for mountain key bosses)
// Returns the resolved boss name and true if successful, empty string and false otherwise
func ResolveBossName(input string) (string, bool) {
	input = strings.ToLower(strings.TrimSpace(input))

	// Try full boss name first
	if IsValidBoss(input) {
		return input, true
	}

	// Try single letter (first letter of boss name)
	if len(input) == 1 {
		for boss := range BossToKey {
			if strings.HasPrefix(boss, input) {
				return boss, true
			}
		}
	}

	// Try key color
	if keyType, ok := ColorToKey[input]; ok {
		// Find the first boss that uses this key type
		// If multiple bosses use the same key, return the first one alphabetically
		var matchingBosses []string
		for boss, key := range BossToKey {
			if key == keyType {
				matchingBosses = append(matchingBosses, boss)
			}
		}
		if len(matchingBosses) > 0 {
			// Return the first one alphabetically for consistency
			// In practice, if multiple bosses share a key, user should be more specific
			bestBoss := matchingBosses[0]
			for _, boss := range matchingBosses[1:] {
				if boss < bestBoss {
					bestBoss = boss
				}
			}
			return bestBoss, true
		}
	}

	return "", false
}

// ResolveKeyType attempts to resolve a key type from various input formats:
// 1. Full key type (e.g., "mountain")
// 2. Full color name (e.g., "brown")
// 3. Boss name/alias (e.g., "griffin" -> "mountain")
// 4. Prefix match on key type (e.g., "m" -> "mountain")
// 5. Prefix match on color name (e.g., "br" -> "brown" -> "mountain")
func ResolveKeyType(input string) (string, bool) {
	input = strings.ToLower(strings.TrimSpace(input))

	// 1. Check if it's a valid key type directly
	for _, key := range BossToKey {
		if key == input {
			return key, true
		}
	}

	// 2. Check if it's a valid color name
	if key, ok := ColorToKey[input]; ok {
		return key, true
	}

	// 3. Check if it resolves to a boss name
	if bossName, ok := ResolveBossName(input); ok {
		if key, ok := GetKeyForBoss(bossName); ok {
			return key, true
		}
	}

	// 4. Prefix match on key type
	for _, key := range BossToKey {
		if strings.HasPrefix(key, input) {
			return key, true
		}
	}

	// 5. Prefix match on color name
	for color, key := range ColorToKey {
		// ColorToKey keys are already lowercased in init()
		// But wait, ColorToKey keys are colors, values are key types
		// The map is color -> key
		if strings.HasPrefix(color, input) {
			return key, true
		}
	}

	return "", false
}
