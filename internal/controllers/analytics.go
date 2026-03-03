package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
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
			Model:       utils.GetDeviceModel(createSessionReq.Model),
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
	var session *models.Session
	var err error

	sessionId := logAppEventReq.SessionID

	if sessionId == "none" {
		session = &models.Session{
			ID: uuid.Nil,
		}
	} else {
		session, err = c.DB.GetSessionById(sessionId)
		if err != nil {
			return nil, err
		}

		if logAppEventReq.EventType == "session_started" {

			session.UpdatedAt = time.Now()
			session.Status = "active"
			_, err = c.DB.UpdateSession(session)
			if err != nil {
				return nil, err
			}
		}

		if logAppEventReq.EventType == "session_ended" {
			session.EndTime = time.Now()
			session.Duration = session.Duration + int64(session.EndTime.Sub(session.UpdatedAt).Seconds())
			session.UpdatedAt = time.Now()
			session.Status = "inactive"
			_, err = c.DB.UpdateSession(session)
			if err != nil {
				return nil, err
			}
		}

		if logAppEventReq.EventType == "url_visited" {
			url, ok := logAppEventReq.Data["url"].(string)
			if !ok {
				return nil, nil
			}
			project, err := c.DB.GetProjectById(session.ProjectID.String())
			if err != nil {
				return nil, err
			}
			if !strings.HasPrefix(url, project.URL) {
				return nil, nil
			}
		}
	}

	appUsageEvent := &models.AppUsageEvent{
		EventType: logAppEventReq.EventType,
		SessionID: session.ID,
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

func (c *Controller) GetActiveDevices(projectId string) ([]responses.GetDeviceResponse, error) {
	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	// get all app uasage events created in 30 minute for the project then filter unique device ids
	events, err := c.DB.GetAppUsageEvents(project)
	if err != nil {
		return nil, err
	}

	var activeDevicesMap = make(map[uuid.UUID]bool)
	var activeDevices []responses.GetDeviceResponse
	timeLimit := time.Now().Add(-30 * time.Minute)
	for _, event := range events {
		if event.CreatedAt.After(timeLimit) {
			session, err := c.DB.GetSessionById(event.SessionID.String())
			if err != nil {
				return nil, err
			}
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

func (c *Controller) GetUniqueDevices(projectId string, timeStart time.Time, timeEnd time.Time) ([]responses.GetDeviceResponse, error) {
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

func (c *Controller) GetSessionsByProject(project *models.Project, timeStart time.Time, timeEnd time.Time) ([]responses.GetSessionResponse, error) {
	sessions, err := c.DB.GetSessionsByProject(project)
	if err != nil {
		return nil, err
	}

	var sessionResponses []responses.GetSessionResponse
	for _, session := range sessions {
		if session.CreatedAt.After(timeStart) && session.CreatedAt.Before(timeEnd) {
			sessionResponses = append(sessionResponses, responses.GetSessionResponse{
				ID:          session.ID,
				ProjectID:   session.ProjectID,
				DeviceID:    session.DeviceID,
				AppVersion:  session.AppVersion,
				IP:          session.IP,
				Geolocation: session.Geolocation,
				Duration:    session.Duration,
				Status:      session.Status,
				CreatedAt:   session.CreatedAt,
			})
		}
	}

	return sessionResponses, nil
}

func (c *Controller) GetProjectEvents(project *models.Project, timeStart time.Time, timeEnd time.Time) ([]responses.GetProjectEventResponse, error) {
	projectEvents, err := c.DB.GetAppUsageEvents(project)
	if err != nil {
		return nil, err
	}

	var projectEventsResponse []responses.GetProjectEventResponse
	for _, event := range projectEvents {
		if event.CreatedAt.After(timeStart) && event.CreatedAt.Before(timeEnd) {
			projectEventsResponse = append(projectEventsResponse, responses.GetProjectEventResponse{
				ID:        event.ID,
				EventType: event.EventType,
				SessionID: event.SessionID,
				Data:      event.Data,
				CreatedAt: event.CreatedAt,
			})
		}
	}

	return projectEventsResponse, nil
}
