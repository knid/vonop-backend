package fly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const flyAPIBase = "https://api.machines.dev/v1"

type Service struct {
	APIToken string
	AppName  string // e.g. "wemob-previews"
	Region   string // e.g. "iad"
	Image    string // e.g. "registry.fly.io/wemob-previews:latest"
	limiter  *rate.Limiter
	client   *http.Client
}

func NewService(apiToken, appName, region, image string) *Service {
	return &Service{
		APIToken: apiToken,
		AppName:  appName,
		Region:   region,
		Image:    image,
		limiter:  rate.NewLimiter(rate.Every(200*time.Millisecond), 5),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- Request / Response types ---

type MachineConfig struct {
	Image    string            `json:"image"`
	Env      map[string]string `json:"env,omitempty"`
	Guest    GuestConfig       `json:"guest"`
	Services []MachineService  `json:"services,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	AutoDestroy bool           `json:"auto_destroy,omitempty"`
}

type GuestConfig struct {
	CPUKind  string `json:"cpu_kind"`
	CPUs     int    `json:"cpus"`
	MemoryMB int    `json:"memory_mb"`
}

type MachineService struct {
	Protocol     string         `json:"protocol"`
	InternalPort int            `json:"internal_port"`
	Ports        []MachinePort  `json:"ports"`
	AutoStop     string         `json:"autostop,omitempty"`
	AutoStart    bool           `json:"autostart,omitempty"`
}

type MachinePort struct {
	Port     int      `json:"port"`
	Handlers []string `json:"handlers"`
}

type CreateMachineRequest struct {
	Name   string        `json:"name,omitempty"`
	Region string        `json:"region,omitempty"`
	Config MachineConfig `json:"config"`
}

type Machine struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	State      string        `json:"state"`
	Region     string        `json:"region"`
	InstanceID string        `json:"instance_id"`
	Config     MachineConfig `json:"config"`
	CreatedAt  string        `json:"created_at"`
	UpdatedAt  string        `json:"updated_at"`
}

// --- HTTP helper ---

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	// Respect Fly's rate limit
	if err := s.limiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	url := fmt.Sprintf("%s/apps/%s%s", flyAPIBase, s.AppName, path)
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		slog.Error("Fly API error", "status", resp.StatusCode, "path", path, "body", string(respBody))
		return nil, fmt.Errorf("fly API error: status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// CreateMachine creates a new Fly Machine with the given configuration.
func (s *Service) CreateMachine(name string, env map[string]string, metadata map[string]string) (*Machine, error) {
	reqBody := CreateMachineRequest{
		Name:   name,
		Region: s.Region,
		Config: MachineConfig{
			Image: s.Image,
			Env:   env,
			Guest: GuestConfig{
				CPUKind:  "shared",
				CPUs:     2,
				MemoryMB: 1024,
			},
			Services: []MachineService{
				{
					Protocol:     "tcp",
					InternalPort: 3000,
					Ports: []MachinePort{
						{Port: 80, Handlers: []string{"http"}},
						{Port: 443, Handlers: []string{"http", "tls"}},
					},
				},
			},
			Metadata: metadata,
		},
	}

	body, err := s.doRequest("POST", "/machines", reqBody)
	if err != nil {
		return nil, err
	}

	var m Machine
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("unmarshal machine: %w", err)
	}
	return &m, nil
}

// GetMachine returns the current state of a Fly Machine.
func (s *Service) GetMachine(machineID string) (*Machine, error) {
	body, err := s.doRequest("GET", fmt.Sprintf("/machines/%s", machineID), nil)
	if err != nil {
		return nil, err
	}

	var m Machine
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("unmarshal machine: %w", err)
	}
	return &m, nil
}

// StartMachine starts a stopped Fly Machine.
func (s *Service) StartMachine(machineID string) error {
	_, err := s.doRequest("POST", fmt.Sprintf("/machines/%s/start", machineID), nil)
	return err
}

// StopMachine sends a stop signal to a running Fly Machine.
func (s *Service) StopMachine(machineID string) error {
	_, err := s.doRequest("POST", fmt.Sprintf("/machines/%s/stop", machineID), nil)
	return err
}

// DestroyMachine permanently destroys a Fly Machine.
func (s *Service) DestroyMachine(machineID string, force bool) error {
	path := fmt.Sprintf("/machines/%s", machineID)
	if force {
		path += "?force=true"
	}
	_, err := s.doRequest("DELETE", path, nil)
	return err
}

// WaitForState blocks until the machine reaches the desired state or the timeout elapses.
// Uses a dedicated HTTP client whose timeout exceeds the Fly API long-poll duration.
func (s *Service) WaitForState(machineID, state string, timeout time.Duration) error {
	if err := s.limiter.Wait(context.Background()); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	url := fmt.Sprintf("%s/apps/%s/machines/%s/wait?state=%s&timeout=%d",
		flyAPIBase, s.AppName, machineID, state, int(timeout.Seconds()))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)

	// HTTP client timeout must exceed the Fly long-poll timeout + buffer
	longPollClient := &http.Client{Timeout: timeout + 15*time.Second}
	resp, err := longPollClient.Do(req)
	if err != nil {
		return fmt.Errorf("wait request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("wait failed: status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListMachines returns all machines in the app.
func (s *Service) ListMachines() ([]Machine, error) {
	body, err := s.doRequest("GET", "/machines", nil)
	if err != nil {
		return nil, err
	}

	var machines []Machine
	if err := json.Unmarshal(body, &machines); err != nil {
		return nil, fmt.Errorf("unmarshal machines: %w", err)
	}
	return machines, nil
}
