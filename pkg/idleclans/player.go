package idleclans

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type SimplePlayer struct {
	SkillExperiencesStr string             `json:"skillExperiences"`
	Skills              map[string]float64 `json:"-"`
	EquipmentStr        string             `json:"equipment"`
	Equipment           []int              `json:"-"`
	HoursOffline        float64            `json:"hoursOffline"`
	TaskTypeOnLogout    int                `json:"taskTypeOnLogout"`
	TaskNameOnLogout    string             `json:"taskNameOnLogout"`
}

type Player struct {
	Username         string `json:"username"`
	GameMode         string `json:"gameMode"`
	GuildName        string `json:"guildName"`
	SkillExperiences struct {
		Attack      float64 `json:"attack"`
		Strength    float64 `json:"strength"`
		Defence     float64 `json:"defence"`
		Archery     float64 `json:"archery"`
		Magic       float64 `json:"magic"`
		Health      float64 `json:"health"`
		Crafting    float64 `json:"crafting"`
		Woodcutting float64 `json:"woodcutting"`
		Carpentry   float64 `json:"carpentry"`
		Fishing     float64 `json:"fishing"`
		Cooking     float64 `json:"cooking"`
		Mining      float64 `json:"mining"`
		Smithing    float64 `json:"smithing"`
		Foraging    float64 `json:"foraging"`
		Farming     float64 `json:"farming"`
		Agility     float64 `json:"agility"`
		Plundering  float64 `json:"plundering"`
		Enchanting  float64 `json:"enchanting"`
		Brewing     float64 `json:"brewing"`
	} `json:"skillExperiences"`
	Equipment struct {
		Boots      int `json:"boots"`
		Jewellery  int `json:"jewellery"`
		Gloves     int `json:"gloves"`
		Legs       int `json:"legs"`
		Body       int `json:"body"`
		LeftHand   int `json:"leftHand"`
		RightHand  int `json:"rightHand"`
		Amulet     int `json:"amulet"`
		Ammunition int `json:"ammunition"`
		Cape       int `json:"cape"`
		Head       int `json:"head"`
		Bracelet   int `json:"bracelet"`
		Belt       int `json:"belt"`
		Pet        int `json:"pet"`
		Earrings   int `json:"earrings"`
	} `json:"equipment"`
	EnchantmentBoosts struct {
		Enchanting  float64 `json:"enchanting"`
		Foraging    float64 `json:"foraging"`
		Farming     float64 `json:"farming"`
		Strength    float64 `json:"strength"`
		Magic       float64 `json:"magic"`
		Archery     float64 `json:"archery"`
		Smithing    float64 `json:"smithing"`
		Crafting    float64 `json:"crafting"`
		Defence     float64 `json:"defence"`
		Fishing     float64 `json:"fishing"`
		Plundering  float64 `json:"plundering"`
		Woodcutting float64 `json:"woodcutting"`
		Attack      float64 `json:"attack"`
		Mining      float64 `json:"mining"`
		Cooking     float64 `json:"cooking"`
		Carpentry   float64 `json:"carpentry"`
		Brewing     float64 `json:"brewing"`
		Agility     float64 `json:"agility"`
	} `json:"enchantmentBoosts"`
	Upgrades struct {
		Housing                int `json:"housing"`
		KeepItSpacious         int `json:"keepItSpacious"`
		TheLumberjack          int `json:"theLumberjack"`
		TheFisherman           int `json:"theFisherman"`
		AutoEating             int `json:"autoEating"`
		AutoLooting            int `json:"autoLooting"`
		OfflineProgress        int `json:"offlineProgress"`
		ValuedClanMember       int `json:"valuedClanMember"`
		FarmingTrickery        int `json:"farmingTrickery"`
		PowerForager           int `json:"powerForager"`
		SmeltingMagic          int `json:"smeltingMagic"`
		MostEfficientFisherman int `json:"mostEfficientFisherman"`
		PlankBargain           int `json:"plankBargain"`
		AmmoSaver              int `json:"ammo-saver"`
		Ninja                  int `json:"ninja"`
		MonsterHunter          int `json:"monsterHunter"`
		Teamwork               int `json:"teamwork"`
		BossSlayer             int `json:"bossSlayer"`
		ToolbeltUpgrade        int `json:"toolbeltUpgrade"`
		LazyRaider             int `json:"lazyRaider"`
		AncientWisdom          int `json:"ancientWisdom"`
		MasterCrafter          int `json:"masterCrafter"`
		ExtraLoadouts          int `json:"extraLoadouts"`
		KronosWho              int `json:"kronosWho?"`
		KeepItBurning          int `json:"keepItBurning"`
		BetterSkinner          int `json:"betterSkinner"`
		BetterFisherman        int `json:"betterFisherman"`
		BetterLumberjack       int `json:"betterLumberjack"`
		ArrowCrafter           int `json:"arrowCrafter"`
		DelicateManufacturing  int `json:"delicateManufacturing"`
		ResponsibleDrinking    int `json:"responsibleDrinking"`
		LastNegotiation        int `json:"lastNegotiation"`
		ShowUsTheMoney         int `json:"showUsTheMoney"`
		PickyEater             int `json:"pickyEater"`
		PrestigiousWoodworking int `json:"prestigiousWoodworking"`
		GettingInSync          int `json:"gettingInSync"`
	} `json:"upgrades"`
	PvmStats struct {
		Griffin               int `json:"Griffin"`
		Devil                 int `json:"Devil"`
		Hades                 int `json:"Hades"`
		Zeus                  int `json:"Zeus"`
		Medusa                int `json:"Medusa"`
		Chimera               int `json:"Chimera"`
		Kronos                int `json:"Kronos"`
		ReckoningOfTheGods    int `json:"ReckoningOfTheGods"`
		GuardiansOfTheCitadel int `json:"GuardiansOfTheCitadel"`
		MalignantSpider       int `json:"MalignantSpider"`
		SkeletonWarrior       int `json:"SkeletonWarrior"`
		OtherworldlyGolem     int `json:"OtherworldlyGolem"`
	} `json:"pvmStats"`
}

var expTable = []int{
	0, 75, 151, 227, 303, 380, 531, 683, 836, 988, 1141, 1294, 1447, 1751, 2054, 2358, 2663, 2967, 3272, 3577,
	4182, 4788, 5393, 5999, 6606, 7212, 7819, 9026, 10233, 11441, 12648, 13856, 15065, 16273, 18682, 21091, 23500, 25910, 28319, 30728, 33140,
	37950, 42761, 47572, 52383, 57195, 62006, 66818, 76431, 86043, 95656, 105269, 114882, 124496, 134109, 153323, 172538, 191752, 210967, 230182,
	249397, 268613, 307028, 345444, 383861, 422277, 460694, 499111, 537528, 614346, 691163, 767981, 844800, 921618, 998437, 1075256, 1228875, 1382495,
	1536114, 1689734, 1843355, 1996975, 2150596, 2457817, 2765038, 3072260, 3379481, 3686703, 3993926, 4301148, 4915571, 5529994, 6144417, 6758841,
	7373264, 7987688, 8602113, 9830937, 11059762, 12288587, 13517412, 14746238, 15975063, 17203889, 19661516, 22119142, 24576769, 27034396, 29492023,
	31949651, 34407278, 39076506, 43737375, 49152963, 54068192, 58983421, 63898650, 68813880, 78644309, 88474739,
}

func GetSkillLevel(exp int) (int, int) {
	for i := len(expTable) - 1; i >= 0; i-- {
		if exp >= expTable[i] {
			if i == len(expTable)-1 {
				return i + 1, 0
			}
			return i + 1, expTable[i+1] - exp
		}
	}
	return 1, expTable[1] - exp
}

// GetPlayer Retrieves the profile for a specific player.
// https://query.idleclans.com/api/Player/profile/{name}
func (c *Client) GetPlayer(ctx context.Context, playerName string) (*Player, error) {
	u, err := c.getBaseURL()
	if err != nil {
		return nil, err
	}

	u.Path = path.Join(u.Path, "Player/profile", playerName)

	req, err := c.getReq(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	ret := &Player{}

	_, err = c.doReq(ctx, req, ret)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

// GetSimplePlayer Retrieves a simplified profile for a specific player.
// https://query.idleclans.com/api/Player/profile/simple/{name}
func (c *Client) GetSimplePlayer(ctx context.Context, playerName string) (*SimplePlayer, error) {
	u, err := c.getBaseURL()
	if err != nil {
		return nil, err
	}

	u.Path = path.Join(u.Path, "Player/profile/simple", playerName)

	fmt.Println(u.String())
	req, err := c.getReq(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	ret := &SimplePlayer{
		Skills: make(map[string]float64),
	}

	ctxzap.Extract(ctx).Info("Requesting player profile", zap.String("url", u.String()))
	_, err = c.doReq(ctx, req, ret)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal([]byte(ret.SkillExperiencesStr), &ret.Skills)
	if err != nil {
		log.Fatalf("Error unmarshalling skill experiences: %v", err)
	}

	err = json.Unmarshal([]byte(ret.EquipmentStr), &ret.Equipment)
	if err != nil {
		log.Fatalf("Error unmarshalling equipment: %v", err)
	}

	return ret, nil
}
