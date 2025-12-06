package quests

import (
	"math"
)

const YoinkBonus = 0.15 // 15% chance to not use a key

// KeyRequirement represents the key requirements for a boss
type KeyRequirement struct {
	KeyType        string
	RequiredKills  int
	RealKeys       int
	EstimatedKeys  int // With yoink bonus
}

// CalculateKeyRequirements calculates key requirements for a set of quests
func CalculateKeyRequirements(quests []Quest) map[string]*KeyRequirement {
	keyReqs := make(map[string]*KeyRequirement)

	for _, quest := range quests {
		keyType, ok := GetKeyForBoss(quest.BossName)
		if !ok {
			continue
		}

		remainingKills := quest.RequiredKills - quest.CurrentKills
		if remainingKills <= 0 {
			continue
		}

		if req, exists := keyReqs[keyType]; exists {
			req.RequiredKills += remainingKills
		} else {
			keyReqs[keyType] = &KeyRequirement{
				KeyType: keyType,
			}
			keyReqs[keyType].RequiredKills = remainingKills
		}
	}

	// Calculate real and estimated keys
	for _, req := range keyReqs {
		req.RealKeys = req.RequiredKills
		// Estimated keys = required_kills * (1 - yoink_bonus) = required_kills * 0.85
		req.EstimatedKeys = int(math.Round(float64(req.RequiredKills) * (1 - YoinkBonus)))
	}

	return keyReqs
}

