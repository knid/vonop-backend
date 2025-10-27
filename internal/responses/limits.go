package responses

import "github.com/google/uuid"

type LimitsResponse struct {
	ID                      uuid.UUID `json:"id"`
	MaximumProjectCount     int       `json:"maximum_project_count"`
	MontlyNotificationCount int       `json:"montly_notification_count"`
	MontlyBuildCount        int       `json:"montly_build_count"`
	CanBuildToAndroid       bool      `json:"can_build_to_android"`
	CanBuildToIOS           bool      `json:"can_build_to_ios"`
	CanUseAnalytics         bool      `json:"can_use_analytics"`
	CanRemoveBrandLogo      bool      `json:"can_remove_brand_logo"`
	CanSeeActiveUsers       bool      `json:"can_see_active_users"`
}
