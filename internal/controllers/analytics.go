package controllers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// resolveClientIP extracts the real client IP from proxy headers, falling back
// to RemoteAddr.
func resolveClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	// Strip port from RemoteAddr (e.g. "1.2.3.4:1234")
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}

// resolveCountryMobile extracts the visitor's country from CDN/proxy headers.
func resolveCountryMobile(r *http.Request) string {
	if c := r.Header.Get("CF-IPCountry"); c != "" {
		return c
	}
	if c := r.Header.Get("X-Vercel-IP-Country"); c != "" {
		return c
	}
	return ""
}

func (c *Controller) CreateSession(r *http.Request, req requests.CreateSessionRequest) (*responses.CreateSessionResponse, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	projectId := chi.URLParam(r, "projectId")
	if _, err := uuid.Parse(projectId); err != nil {
		return nil, fmt.Errorf("invalid project id")
	}

	project, err := c.DB.GetProjectById(projectId)
	if err != nil {
		return nil, err
	}

	resolvedModel := utils.GetDeviceModel(req.Model)

	device, err := c.DB.GetDeviceByToken(req.DeviceID.String())
	if err != nil {
		// Device not found — create it
		deviceModel := &models.Device{
			DeviceToken: req.DeviceID,
			Platform:    req.Platform,
			Model:       resolvedModel,
		}
		device, err = c.DB.CreateDevice(deviceModel)
		if err != nil {
			return nil, err
		}
	} else {
		// Device exists — update platform/model if changed (B20)
		changed := false
		if device.Platform != req.Platform {
			device.Platform = req.Platform
			changed = true
		}
		if device.Model != resolvedModel {
			device.Model = resolvedModel
			changed = true
		}
		if changed {
			_, _ = c.DB.UpdateDevice(device)
		}
	}

	now := time.Now()
	session := &models.Session{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		AppVersion:  req.AppVersion,
		IP:          resolveClientIP(r),
		Geolocation: resolveCountryMobile(r),
		StartTime:   now,
		EndTime:     now,
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

func (c *Controller) LogAppEvent(r *http.Request, req requests.LogAppEventRequest) error {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return err
	}

	var sessionID uuid.UUID

	if req.SessionID == "none" {
		// Sessionless event (push notifications) — use nil UUID but still record
		sessionID = uuid.Nil
	} else {
		parsedID, err := uuid.Parse(req.SessionID)
		if err != nil {
			return fmt.Errorf("invalid session_id")
		}
		sessionID = parsedID

		session, err := c.DB.GetSessionById(req.SessionID)
		if err != nil {
			return fmt.Errorf("session not found")
		}

		switch req.EventType {
		case "session_started":
			session.Status = "active"
			session.UpdatedAt = time.Now()
			_ = c.DB.UpdateSessionFields(session, "status", "updated_at")

		case "session_ended":
			now := time.Now()
			elapsed := int64(now.Sub(session.UpdatedAt).Seconds())
			if elapsed < 0 {
				elapsed = 0
			}
			session.EndTime = now
			session.Duration = session.Duration + elapsed
			session.UpdatedAt = now
			session.Status = "inactive"
			_ = c.DB.UpdateSessionFields(session, "end_time", "duration", "updated_at", "status")

		case "ping":
			// Pings also re-activate sessions that were timed out by the job (B10)
			if session.Status != "active" {
				session.Status = "active"
				session.UpdatedAt = time.Now()
				_ = c.DB.UpdateSessionFields(session, "status", "updated_at")
			}

		case "url_visited":
			url, ok := req.Data["url"].(string)
			if !ok || url == "" {
				return nil // silently skip — no URL provided
			}
			project, err := c.DB.GetProjectById(session.ProjectID.String())
			if err != nil {
				return nil
			}
			if !strings.HasPrefix(url, project.URL) {
				return nil // URL doesn't match project — skip event
			}
		}
	}

	appUsageEvent := &models.AppUsageEvent{
		EventType: req.EventType,
		SessionID: sessionID,
		Data:      req.Data,
	}

	_, err := c.DB.CreateAppUsageEvent(appUsageEvent)
	return err
}

func (c *Controller) GetDevicesByProjectId(projectId string) ([]responses.GetDeviceResponse, error) {
	devices, err := c.DB.GetDevicesByProject(projectId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.GetDeviceResponse, 0, len(devices))
	for _, device := range devices {
		result = append(result, responses.GetDeviceResponse{
			ID:        device.ID,
			Platform:  device.Platform,
			Model:     device.Model,
			CreatedAt: device.CreatedAt.String(),
		})
	}
	return result, nil
}

func (c *Controller) GetActiveDevices(projectId string) ([]responses.GetDeviceResponse, error) {
	devices, err := c.DB.GetActiveDevicesByProject(projectId, 30*time.Minute)
	if err != nil {
		return nil, err
	}

	result := make([]responses.GetDeviceResponse, 0, len(devices))
	for _, device := range devices {
		result = append(result, responses.GetDeviceResponse{
			ID:        device.ID,
			Platform:  device.Platform,
			Model:     device.Model,
			CreatedAt: device.CreatedAt.String(),
		})
	}
	return result, nil
}

func (c *Controller) GetUniqueDevices(projectId string, timeStart, timeEnd time.Time) ([]responses.GetDeviceResponse, error) {
	devices, err := c.DB.GetUniqueDevicesByProject(projectId, timeStart, timeEnd)
	if err != nil {
		return nil, err
	}

	result := make([]responses.GetDeviceResponse, 0, len(devices))
	for _, device := range devices {
		result = append(result, responses.GetDeviceResponse{
			ID:        device.ID,
			Platform:  device.Platform,
			Model:     device.Model,
			CreatedAt: device.CreatedAt.String(),
		})
	}
	return result, nil
}

func (c *Controller) GetSessionsByProject(projectId string, timeStart, timeEnd time.Time) ([]responses.GetSessionResponse, error) {
	sessions, err := c.DB.GetSessionsByProject(projectId, timeStart, timeEnd)
	if err != nil {
		return nil, err
	}

	result := make([]responses.GetSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, responses.GetSessionResponse{
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
	return result, nil
}

func (c *Controller) GetProjectEvents(projectId string, timeStart, timeEnd time.Time) ([]responses.GetProjectEventResponse, error) {
	events, err := c.DB.GetAppUsageEventsByProject(projectId, timeStart, timeEnd)
	if err != nil {
		return nil, err
	}

	result := make([]responses.GetProjectEventResponse, 0, len(events))
	for _, event := range events {
		result = append(result, responses.GetProjectEventResponse{
			ID:        event.ID,
			EventType: event.EventType,
			SessionID: event.SessionID,
			Data:      event.Data,
			CreatedAt: event.CreatedAt,
		})
	}
	return result, nil
}
