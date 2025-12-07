export interface Quest {
  boss_name: string;
  required_kills: number;
  current_kills: number;
}

export interface UserData {
  discord_id: string;
  username: string;
  avatar: string;
  player_name: string;
  alts: string[];
  quests: Record<string, Quest[]>;
  keys: Record<string, Record<string, number>>; // player name -> key type -> count
}

export interface PlayerData {
  discord_id: string;
  player_name: string;
  is_alt: boolean;
  owner_name?: string;
  alts: string[];
  quests: Quest[];
  keys: Record<string, number>;
}

// Clan view types
export interface ClanBossEntry {
  player_name: string;
  remaining_kills: number;
}

export interface ClanBossData {
  week: number;
  year: number;
  bosses: Record<string, ClanBossEntry[]>;
}

export interface ClanKeyEntry {
  player_name: string;
  count: number;
}

export interface ClanKeysData {
  keys: Record<string, ClanKeyEntry[]>;
}

// Plan types
export interface PlanPartyTask {
  boss_name: string;
  kills: number;
  key_holder?: string;
  key_type: string;
  no_keys: boolean;
}

export interface PlanParty {
  players: string[];
  tasks: PlanPartyTask[];
}

export interface PlanLeftover {
  player_name: string;
  needs: Record<string, number>;
}

export interface PlanData {
  week: number;
  year: number;
  parties: PlanParty[];
  leftovers: PlanLeftover[];
}

export const BOSSES = [
  { name: 'griffin', key: 'mountain', color: '#8B4513', label: 'Griffin' },
  { name: 'medusa', key: 'stone', color: '#808080', label: 'Medusa' },
  { name: 'hades', key: 'underworld', color: '#4169E1', label: 'Hades' },
  { name: 'zeus', key: 'godly', color: '#FFD700', label: 'Zeus' },
  { name: 'devil', key: 'burning', color: '#DC143C', label: 'Devil' },
  { name: 'chimera', key: 'mutated', color: '#228B22', label: 'Chimera' },
  { name: 'dragon', key: 'otherworldly', color: '#9400D3', label: 'Dragon' },
  { name: 'sobek', key: 'ancient', color: '#DAA520', label: 'Sobek' },
  { name: 'kronos', key: 'kronos', color: '#4A0080', label: 'Kronos' },
] as const;

export const KEY_TYPES = [
  { type: 'mountain', color: '#8B4513', label: 'Mountain (Brown)' },
  { type: 'stone', color: '#808080', label: 'Stone (Gray)' },
  { type: 'underworld', color: '#4169E1', label: 'Underworld (Blue)' },
  { type: 'godly', color: '#FFD700', label: 'Godly (Gold)' },
  { type: 'burning', color: '#DC143C', label: 'Burning (Red)' },
  { type: 'mutated', color: '#228B22', label: 'Mutated (Green)' },
  { type: 'otherworldly', color: '#9400D3', label: 'Otherworldly' },
  { type: 'ancient', color: '#DAA520', label: 'Ancient' },
  { type: 'kronos', color: '#4A0080', label: 'Kronos (Book)' },
] as const;

