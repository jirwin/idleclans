package quests

import (
	"context"
	"fmt"
	"sort"
)

// Planner generates a party plan for boss quests
type Planner struct {
	db *DB
}

func NewPlanner(db *DB) *Planner {
	return &Planner{db: db}
}

// PlayerProfile tracks a player's needs and available keys
type PlayerProfile struct {
	Name      string
	Needs     map[string]int // Boss -> Kills needed
	Keys      map[string]int // KeyType -> Count
	TotalNeed int            // Total kills needed
}

// Party represents a group of players assigned to tasks
type Party struct {
	Players []string
	Tasks   []PartyTask
}

// PartyTask represents a boss assignment for a party
type PartyTask struct {
	BossName  string
	Kills     int
	KeyHolder string // Player providing the key (empty if no keys available)
	KeyType   string
	NoKeys    bool // True if no keys available for this task
}

// PlanResult contains the generated plan
type PlanResult struct {
	Parties   []Party
	Leftovers []PlayerProfile // Players with remaining unmet needs
}

// GeneratePlan creates an efficient party schedule
func (p *Planner) GeneratePlan(ctx context.Context, weekNumber, year int) (*PlanResult, error) {
	return p.GeneratePlanFiltered(ctx, weekNumber, year, nil)
}

// GeneratePlanFiltered creates a plan filtered to specific players (nil = all players)
func (p *Planner) GeneratePlanFiltered(ctx context.Context, weekNumber, year int, onlinePlayers []string) (*PlanResult, error) {
	// Build a set of online players for quick lookup
	onlineSet := make(map[string]bool)
	filterByOnline := len(onlinePlayers) > 0
	for _, name := range onlinePlayers {
		onlineSet[name] = true
	}
	// 1. Fetch all quests for the week
	questsList, err := p.db.GetAllQuestsForWeek(ctx, weekNumber, year)
	if err != nil {
		return nil, fmt.Errorf("failed to get quests: %w", err)
	}

	if len(questsList) == 0 {
		return &PlanResult{}, nil
	}

	// 2. Fetch all player keys
	allKeys, err := p.db.GetAllPlayerKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get player keys: %w", err)
	}

	// 3. Build Player Profiles - include ALL players (with quests or keys)
	profiles := make(map[string]*PlayerProfile)

	// Initialize profiles from quests (Needs)
	for _, q := range questsList {
		remaining := q.RequiredKills - q.CurrentKills
		if remaining <= 0 {
			continue
		}

		if _, exists := profiles[q.PlayerName]; !exists {
			profiles[q.PlayerName] = &PlayerProfile{
				Name:  q.PlayerName,
				Needs: make(map[string]int),
				Keys:  make(map[string]int),
			}
		}
		profiles[q.PlayerName].Needs[q.BossName] = remaining
		profiles[q.PlayerName].TotalNeed += remaining
	}

	// Add keys to profiles (include players who only have keys, no quests)
	for _, k := range allKeys {
		if _, exists := profiles[k.PlayerName]; !exists {
			profiles[k.PlayerName] = &PlayerProfile{
				Name:  k.PlayerName,
				Needs: make(map[string]int),
				Keys:  make(map[string]int),
			}
		}
		profiles[k.PlayerName].Keys[k.KeyType] = k.Count
	}

	// Separate players into those with needs and available helpers
	// If filtering by online, only include online players
	var playersWithNeeds []*PlayerProfile
	var availableHelpers []*PlayerProfile

	for _, prof := range profiles {
		// Skip players not in the online filter
		if filterByOnline && !onlineSet[prof.Name] {
			continue
		}
		
		if prof.TotalNeed > 0 {
			playersWithNeeds = append(playersWithNeeds, prof)
		} else {
			availableHelpers = append(availableHelpers, prof)
		}
	}

	// Sort players with needs by TotalNeed descending (hardest to satisfy first)
	sort.Slice(playersWithNeeds, func(i, j int) bool {
		return playersWithNeeds[i].TotalNeed > playersWithNeeds[j].TotalNeed
	})

	// Sort helpers by total keys descending (most useful helpers first)
	sort.Slice(availableHelpers, func(i, j int) bool {
		totalKeysI := 0
		for _, c := range availableHelpers[i].Keys {
			totalKeysI += c
		}
		totalKeysJ := 0
		for _, c := range availableHelpers[j].Keys {
			totalKeysJ += c
		}
		return totalKeysI > totalKeysJ
	})

	// Track which players have been assigned to parties
	assigned := make(map[string]bool)

	// 4. Party Formation (Greedy) - always try to form parties of 3
	var parties []Party

	for len(playersWithNeeds) > 0 {
		// Find the best group starting with the player with highest need
		bestGroup := findBestGroupWithHelpers(playersWithNeeds, availableHelpers, profiles, assigned)
		if len(bestGroup.Players) == 0 {
			break
		}

		party := Party{
			Players: bestGroup.Players,
			Tasks:   make([]PartyTask, 0),
		}

		// Mark players as assigned for this round
		for _, pName := range bestGroup.Players {
			assigned[pName] = true
		}

		// Assign tasks for this group - for ALL bosses that anyone in the group needs
		bossesNeeded := make(map[string]int) // boss -> max kills needed
		for _, pName := range bestGroup.Players {
			for boss, need := range profiles[pName].Needs {
				if need > 0 {
					if need > bossesNeeded[boss] {
						bossesNeeded[boss] = need
					}
				}
			}
		}

		// Sort bosses for consistent output
		var bossList []string
		for boss := range bossesNeeded {
			bossList = append(bossList, boss)
		}
		sort.Strings(bossList)

		for _, boss := range bossList {
			killsNeeded := bossesNeeded[boss]

			// Check for keys within the party
			keyType, _ := GetKeyForBoss(boss)
			keysAvailable := 0
			for _, pName := range bestGroup.Players {
				keysAvailable += profiles[pName].Keys[keyType]
			}

			if keysAvailable > 0 {
				// Use available keys
				killsToDo := killsNeeded
				if keysAvailable < killsToDo {
					killsToDo = keysAvailable
				}

				// Assign key holder(s)
				remainingKillsToPay := killsToDo

				// Sort players by key count descending
				payers := make([]string, len(bestGroup.Players))
				copy(payers, bestGroup.Players)
				sort.Slice(payers, func(i, j int) bool {
					return profiles[payers[i]].Keys[keyType] > profiles[payers[j]].Keys[keyType]
				})

				for _, payer := range payers {
					if remainingKillsToPay <= 0 {
						break
					}

					has := profiles[payer].Keys[keyType]
					if has > 0 {
						pay := has
						if pay > remainingKillsToPay {
							pay = remainingKillsToPay
						}

						// Record task
						party.Tasks = append(party.Tasks, PartyTask{
							BossName:  boss,
							Kills:     pay,
							KeyHolder: payer,
							KeyType:   keyType,
						})

						// Deduct keys
						profiles[payer].Keys[keyType] -= pay
						remainingKillsToPay -= pay
					}
				}

				// Deduct needs for everyone in party
				for _, pName := range bestGroup.Players {
					if profiles[pName].Needs[boss] > 0 {
						profiles[pName].Needs[boss] -= killsToDo
						if profiles[pName].Needs[boss] < 0 {
							profiles[pName].Needs[boss] = 0
						}
					}
				}
			} else {
				// No keys available - still record the task but mark as needing keys
				party.Tasks = append(party.Tasks, PartyTask{
					BossName: boss,
					Kills:    killsNeeded,
					KeyType:  keyType,
					NoKeys:   true,
				})

				// Still deduct needs (they'll figure out keys)
				for _, pName := range bestGroup.Players {
					if profiles[pName].Needs[boss] > 0 {
						profiles[pName].Needs[boss] -= killsNeeded
						if profiles[pName].Needs[boss] < 0 {
							profiles[pName].Needs[boss] = 0
						}
					}
				}
			}
		}

		// Sort tasks by key holder so each player uses all their keys contiguously
		// This minimizes the number of times the party leader needs to swap key providers
		sort.Slice(party.Tasks, func(i, j int) bool {
			// Kronos always goes last
			if party.Tasks[i].BossName == "kronos" {
				return false
			}
			if party.Tasks[j].BossName == "kronos" {
				return true
			}
			// NoKeys tasks go last (but before kronos)
			if party.Tasks[i].NoKeys != party.Tasks[j].NoKeys {
				return !party.Tasks[i].NoKeys
			}
			// Sort by key holder name
			if party.Tasks[i].KeyHolder != party.Tasks[j].KeyHolder {
				return party.Tasks[i].KeyHolder < party.Tasks[j].KeyHolder
			}
			// Within same key holder, sort by boss name for consistency
			return party.Tasks[i].BossName < party.Tasks[j].BossName
		})

		// Add party to plan (always, even if no keys)
		if len(party.Tasks) > 0 {
			parties = append(parties, party)
		}

		// Re-evaluate players with needs
		var newPlayersWithNeeds []*PlayerProfile
		for _, prof := range playersWithNeeds {
			total := 0
			for _, n := range prof.Needs {
				total += n
			}
			prof.TotalNeed = total
			if total > 0 && !assigned[prof.Name] {
				newPlayersWithNeeds = append(newPlayersWithNeeds, prof)
			}
		}
		playersWithNeeds = newPlayersWithNeeds

		// Re-sort
		sort.Slice(playersWithNeeds, func(i, j int) bool {
			return playersWithNeeds[i].TotalNeed > playersWithNeeds[j].TotalNeed
		})

		// Update available helpers (remove assigned ones)
		var newHelpers []*PlayerProfile
		for _, h := range availableHelpers {
			if !assigned[h.Name] {
				newHelpers = append(newHelpers, h)
			}
		}
		availableHelpers = newHelpers
	}

	// Gather leftovers (players with remaining needs)
	var leftovers []PlayerProfile
	for _, prof := range profiles {
		total := 0
		for _, n := range prof.Needs {
			total += n
		}
		if total > 0 {
			leftovers = append(leftovers, *prof)
		}
	}

	return &PlanResult{
		Parties:   parties,
		Leftovers: leftovers,
	}, nil
}

type GroupCandidate struct {
	Players  []string
	Overlaps []string // Bosses needed by members
	Score    int
}

// findBestGroupWithHelpers finds a group of 3 players, prioritizing overlap but filling with helpers
func findBestGroupWithHelpers(playersWithNeeds, helpers []*PlayerProfile, allProfiles map[string]*PlayerProfile, assigned map[string]bool) GroupCandidate {
	// Filter out already assigned players
	var available []*PlayerProfile
	for _, p := range playersWithNeeds {
		if !assigned[p.Name] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return GroupCandidate{}
	}

	// Pick the first player (highest need) as anchor
	anchor := available[0]
	candidate := GroupCandidate{
		Players: []string{anchor.Name},
	}

	// Find partners with best overlap from players with needs
	type PartnerScore struct {
		Name         string
		Score        int
		SharedBosses []string
	}

	var partners []PartnerScore

	for i := 1; i < len(available); i++ {
		other := available[i]
		score := 0
		var shared []string

		for boss, need := range anchor.Needs {
			if need > 0 && other.Needs[boss] > 0 {
				score++
				shared = append(shared, boss)
			}
		}

		// Give some score even if no overlap (we want to fill parties)
		partners = append(partners, PartnerScore{
			Name:         other.Name,
			Score:        score,
			SharedBosses: shared,
		})
	}

	// Sort partners by score (highest overlap first)
	sort.Slice(partners, func(i, j int) bool {
		return partners[i].Score > partners[j].Score
	})

	// Take up to 2 partners from players with needs
	for i := 0; i < 2 && i < len(partners); i++ {
		candidate.Players = append(candidate.Players, partners[i].Name)
	}

	// If we don't have 3 players yet, add helpers
	for _, helper := range helpers {
		if len(candidate.Players) >= 3 {
			break
		}
		if !assigned[helper.Name] {
			candidate.Players = append(candidate.Players, helper.Name)
		}
	}

	// Collect all bosses that anyone in the group needs
	var allBosses []string
	seenBosses := make(map[string]bool)
	for _, pName := range candidate.Players {
		for boss, need := range allProfiles[pName].Needs {
			if need > 0 && !seenBosses[boss] {
				allBosses = append(allBosses, boss)
				seenBosses[boss] = true
			}
		}
	}
	candidate.Overlaps = allBosses

	return candidate
}
