package external

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
		return "", errors.New("ERRR")
	}

	return onesignal_app_id.(string), nil
}
