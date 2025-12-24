package external

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/knid/wemob/internal/models"
)

func CreateOneSignalApplication(project models.Project) (string, error) {
	url := "https://api.onesignal.com/apps"

	payloadStr := map[string]string{
		"name":                        project.PackageName,
		"organization_id":             os.Getenv("ONESIGNAL_ORG_ID"),
		"fcm_v1_service_account_json": os.Getenv("FCM_V1_SERVICE_ACCOUNT_JSON_B64"),
	}
	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+os.Getenv("ONESIGNAL_ORG_API_KEY"))

	res, _ := http.DefaultClient.Do(req)

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	onesignal_app_id, ok := response["id"]
	if !ok {
		slog.Error("Error creating OneSignal Application", "response", response, "project_id", project.ID)
		return "", errors.New("error creating application")
	}

	return onesignal_app_id.(string), nil
}

func CreateOneSignalAppToken(project models.Project) (string, error) {
	url := fmt.Sprintf("https://api.onesignal.com/apps/%s/auth/tokens", project.OneSignalAppID)

	payloadStr := map[string]string{
		"name": "wemob-api",
	}

	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+os.Getenv("ONESIGNAL_ORG_API_KEY"))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	onesignal_app_api_key, ok := response["formatted_token"]
	if !ok {
		slog.Error("Error creating OneSignal App Token", "response", response, "project_id", project.ID)
		return "", errors.New("error creating app token")
	}

	return onesignal_app_api_key.(string), nil
}

func UploadAPNSP8KeyToOneSignal(project models.Project, apnsP8Key string, apnsKeyID string, apnsTeamID string, apnsBundleID string) error {
	url := fmt.Sprintf("https://api.onesignal.com/apps/%s", project.OneSignalAppID)
	payloadStr := map[string]string{
		"organization_id": os.Getenv("ONESIGNAL_ORG_ID"),
		"apns_p8":         apnsP8Key,
		"apns_key_id":     apnsKeyID,
		"apns_team_id":    apnsTeamID,
		"apns_bundle_id":  apnsBundleID,
	}
	marshalled, _ := json.Marshal(payloadStr)

	fmt.Println("payload: ", payloadStr)

	req, _ := http.NewRequest("PUT", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+os.Getenv("ONESIGNAL_ORG_API_KEY"))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	if res.StatusCode != 200 {
		slog.Error("Error uploading APNS P8 Key to OneSignal", "response", response, "project_id", project.ID)
		return errors.New("error uploading APNS P8 Key to OneSignal")
	}

	return nil
}

func SendOneSignalNotification(notification *models.Notification, project *models.Project) error {
	url := "https://api.onesignal.com/notifications?c=push"

	payloadStr := map[string]any{
		"app_id": project.OneSignalAppID,
		"contents": map[string]string{
			"en": notification.Description,
		},
		"headings": map[string]string{
			"en": notification.Title,
		},
		"data": map[string]string{
			"notification_id": notification.ID.String(),
		},
		"included_segments": []string{
			"Total Subscriptions",
		},
	}
	if notification.Image != "" {
		imgUrl := "https://old-clocks-lick.loca.lt/v1/static/images/" + notification.ID.String() + ".png"
		payloadStr["ios_attachments"] = map[string]string{
			"id": imgUrl,
		}
		payloadStr["big_picture"] = imgUrl
	}

	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+project.OneSignalAppToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	return nil
}
