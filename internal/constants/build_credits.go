package constants

// BuildCreditKind names a chargeable build action. The string values are
// also used as the `action` argument to DeductAICreditsUnchecked so they
// show up as a recognizable label in credit-ledger rows.
type BuildCreditKind string

const (
	BuildCreditKindAndroid BuildCreditKind = "build_android"
	BuildCreditKindIOS     BuildCreditKind = "build_ios"
	BuildCreditKindOTA     BuildCreditKind = "ota_publish"
)

// BuildCreditCosts maps each chargeable action to its credit cost.
// Derived from Codemagic pay-as-you-go pricing:
//   - Android (linux_x2, ~$0.038/min × ~8 min) ≈ $0.30 / 13 cr at $0.0225/cr → 20 cr (35% margin)
//   - iOS (mac_mini_m1, ~$0.038/min × ~12 min) ≈ $0.45 / 20 cr → 30 cr (33% margin)
//   - OTA (our Fly worker, ~$0.01) ≈ 0.5 cr → 3 cr (85% margin, intentionally cheap)
var BuildCreditCosts = map[BuildCreditKind]int{
	BuildCreditKindAndroid: 20,
	BuildCreditKindIOS:     30,
	BuildCreditKindOTA:     3,
}
