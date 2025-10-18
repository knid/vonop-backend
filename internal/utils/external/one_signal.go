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
		"name":            project.PackageName,
		"organization_id": os.Getenv("ONESIGNAL_ORG_ID"),
	}
	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+os.Getenv("ONESIGNAL_ORG_API_KEY"))

	res, _ := http.DefaultClient.Do(req)

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

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

	res, _ := http.DefaultClient.Do(req)

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	onesignal_app_api_key, ok := response["formatted_token"]
	if !ok {
		fmt.Println("**************************")
		fmt.Println(response)
		fmt.Println(project.OneSignalAppID)
		slog.Error("Error creating OneSignal App Token", "response", response, "project_id", project.ID)
		return "", errors.New("error creating app token")
	}

	return onesignal_app_api_key.(string), nil
}

func SendOneSignalNotification(notification *models.Notification, appId string) error {
	url := "https://api.onesignal.com/notifications?c=push"

	payloadStr := map[string]any{
		"app_id": appId,
		"contents": map[string]string{
			"en": notification.Description,
		},
		"headings": map[string]string{
			"en": notification.Title,
		},
		"data": map[string]string{
			"notification_id": notification.ID.String(),
		},
	}
	if notification.Image != "" {
		imgUrl := "https://api.wemob.io/v1/static/images/" + notification.Image + ".png"
		payloadStr["ios_attachments"] = map[string]string{
			"id": imgUrl,
		}
		payloadStr["big_picture"] = imgUrl
	}

	marshalled, _ := json.Marshal(payloadStr)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(marshalled))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+os.Getenv("ONESIGNAL_ORG_API_KEY"))

	res, _ := http.DefaultClient.Do(req)

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var response map[string]interface{}
	json.Unmarshal(body, &response)

	return nil
}
