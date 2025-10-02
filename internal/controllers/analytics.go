package controllers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) LogAppEvent(r *http.Request, logAppEventReq requests.LogAppEventRequest) (*responses.LogAppEventResponse, error) {
	projectId := chi.URLParam(r, "projectId")
	err := uuid.Validate(projectId)
	if projectId == "" {
		return nil, err
	}
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	_, err = c.DB.GetDeviceById(logAppEventReq.DeviceID.String())
	if err != nil {
		device := &models.Device{
			DeviceToken: logAppEventReq.DeviceID,
			Platform:    logAppEventReq.Platform,
			Model:       logAppEventReq.Model,
		}
		_, err = c.DB.CreateDevice(device)
		if err != nil {
			return nil, err
		}
	}

	appUsageEvent := &models.AppUsageEvent{
		EventType:  logAppEventReq.EventType,
		ProjectID:  project.ID,
		DeviceID:   logAppEventReq.DeviceID,
		AppVersion: logAppEventReq.AppVersion,
		IP:         r.RemoteAddr,
		// Geolocation: TODO: Implement geolocation lookup based on IP --- IGNORE ---
	}

	_, err = c.DB.CreateAppUsageEvent(appUsageEvent)
	if err != nil {
		return nil, err
	}

	return &responses.LogAppEventResponse{
		SuccessResponse: responses.SuccessResponse{
			Success: true,
			Error:   "",
		},
	}, nil
}

func (c *Controller) GetDevicesByProjectId(projectId string) ([]responses.GetDeviceResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	devices, err := c.DB.GetDevicesByProject(project)
	if err != nil {
		return nil, err
	}

	var getDevicesResponse responses.GetDevicesResponse
	for _, device := range devices {
		getDevicesResponse.Devices = append(getDevicesResponse.Devices, responses.GetDeviceResponse{
			ID:        device.ID,
			Platform:  device.Platform,
			Model:     device.Model,
			CreatedAt: device.CreatedAt.String(),
		})
	}

	return getDevicesResponse.Devices, nil
}

func (c *Controller) GetActiveUsers(projectId string) ([]responses.GetDeviceResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	// get all app uasage events created in 30 minute for the project then filter unique device ids
	appUsageEvents, err := c.DB.GetAppUsageEvents(project)
	if err != nil {
		return nil, err
	}

	var activeDevicesMap = make(map[uuid.UUID]bool)
	var activeDevices []responses.GetDeviceResponse
	timeLimit := time.Now().Add(-30 * time.Minute)
	for _, appUsageEvent := range *appUsageEvents {
		if appUsageEvent.CreatedAt.After(timeLimit) {
			if _, exists := activeDevicesMap[appUsageEvent.DeviceID]; !exists {
				activeDevicesMap[appUsageEvent.DeviceID] = true
				device, err := c.DB.GetDeviceById(appUsageEvent.DeviceID.String())
				if err != nil {
					return nil, err
				}
				activeDevices = append(activeDevices, responses.GetDeviceResponse{
					ID:        device.ID,
					Platform:  device.Platform,
					Model:     device.Model,
					CreatedAt: device.CreatedAt.String(),
				})
			}
		}
	}

	return activeDevices, nil
}

func (c *Controller) GetUniqueUsers(projectId string, timeStart time.Time, timeEnd time.Time) ([]responses.GetDeviceResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	devices, err := c.DB.GetDevicesByProject(project)
	if err != nil {
		return nil, err
	}

	// filter devices by created time between timeStart and timeEnd
	var uniqueDevices []responses.GetDeviceResponse
	for _, device := range devices {
		if device.CreatedAt.After(timeStart) && device.CreatedAt.Before(timeEnd) {
			uniqueDevices = append(uniqueDevices, responses.GetDeviceResponse{
				ID:        device.ID,
				Platform:  device.Platform,
				Model:     device.Model,
				CreatedAt: device.CreatedAt.String(),
			})
		}
	}

	return uniqueDevices, nil
}
