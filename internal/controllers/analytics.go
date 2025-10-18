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

func (c *Controller) CreateSession(r *http.Request, createSessionReq requests.CreateSessionRequest) (*responses.CreateSessionResponse, error) {
	projectId := chi.URLParam(r, "projectId")
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	device, err := c.DB.GetDeviceByToken(createSessionReq.DeviceID.String())
	if err != nil {
		deviceModel := &models.Device{
			DeviceToken: createSessionReq.DeviceID,
			Platform:    createSessionReq.Platform,
			Model:       createSessionReq.Model,
		}
		device, err = c.DB.CreateDevice(deviceModel)
		if err != nil {
			return nil, err
		}
	}

	session := &models.Session{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		AppVersion:  createSessionReq.AppVersion,
		IP:          r.RemoteAddr,
		Geolocation: "",
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		Duration:    0,
		Status:      "active",
	}

	createdSession, err := c.DB.CreateSession(session)
	if err != nil {
		return nil, err
	}

	return &responses.CreateSessionResponse{
		SessionID: createdSession.ID,
	}, nil
}

func (c *Controller) LogAppEvent(r *http.Request, logAppEventReq requests.LogAppEventRequest) (*models.AppUsageEvent, error) {
	var session models.Session
	var err error

	sessionId := logAppEventReq.SessionID.String()

	if sessionId == "none" {
		session = models.Session{
			ID: uuid.Nil,
		}
	} else {
		session, err = c.DB.GetSessionById(sessionId)
		if err != nil {
			return nil, err
		}
	}
	appUsageEvent := &models.AppUsageEvent{
		EventType: logAppEventReq.EventType,
		SessionID: session.ID.String(),
		Data:      logAppEventReq.Data,
	}

	createdAppUsageEvent, err := c.DB.CreateAppUsageEvent(appUsageEvent)
	if err != nil {
		return nil, err
	}

	return createdAppUsageEvent, nil
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
	sessions, err := c.DB.GetSessionsByProject(project)
	if err != nil {
		return nil, err
	}

	var activeDevicesMap = make(map[uuid.UUID]bool)
	var activeDevices []responses.GetDeviceResponse
	timeLimit := time.Now().Add(-30 * time.Minute)
	for _, session := range sessions {
		if session.CreatedAt.After(timeLimit) {
			if _, exists := activeDevicesMap[session.DeviceID]; !exists {
				activeDevicesMap[session.DeviceID] = true
				device, err := c.DB.GetDeviceById(session.DeviceID.String())
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
