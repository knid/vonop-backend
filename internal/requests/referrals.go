package requests

type ApplyReferralCodeRequest struct {
	Code string `json:"code" validate:"required" errormgs:"referral code is required"`
}
