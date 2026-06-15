package session

import "99dps/internal/combat"

// hit is a tiny helper to push a melee damage line through the manager.
func hit(sm *SessionManager, at int64, dealer string, dmg int, target string) {
	sm.Apply(&combat.DamageSet{ActionTime: at, Dealer: dealer, Dmg: dmg, Target: target})
}
