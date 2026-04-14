package views

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
)

// gatewayHTTPClient is reused across gateway proxy requests for connection pooling.
var gatewayHTTPClient = &http.Client{}

// GatewayModels handles GET /v2/gateway/models.
// Returns the list of chat and image models available to the authenticated user.
func (v *HttpViews) GatewayModels(w http.ResponseWriter, r *http.Request) {
	user := middleware.GatewayUserFromContext(r.Context())
	if user == nil {
		middleware.GatewayError(w, "unauthorized", "authentication_error", "invalid_api_key", http.StatusUnauthorized)
		return
	}

	allowed := v.Controller.GatewayAllowedModels(user)
	allowedSet := make(map[string]bool, len(allowed))
	for _, m := range allowed {
		allowedSet[m] = true
	}

	var chat []constants.GatewayModelInfo
	var image []constants.GatewayModelInfo

	for _, m := range constants.GatewayModelCatalog {
		if !allowedSet[m.ID] {
			continue
		}
		info := m
		// Override image credit costs from remote config
		if info.Type == "image" {
			info.CostCredits = v.Controller.GatewayImageCreditCost(info.ID)
		}
		if info.Type == "chat" {
			chat = append(chat, info)
		} else if info.Type == "image" {
			image = append(image, info)
		}
	}

	if chat == nil {
		chat = []constants.GatewayModelInfo{}
	}
	if image == nil {
		image = []constants.GatewayModelInfo{}
	}

	utils.JSONResponse(w, responses.GatewayModelsResponse{
		Chat:  chat,
		Image: image,
	}, http.StatusOK)
}

// GatewayChat handles POST /v2/gateway/chat/completions.
// Proxies an OpenAI-compatible chat request to the appropriate upstream provider.
func (v *HttpViews) GatewayChat(w http.ResponseWriter, r *http.Request) {
	user := middleware.GatewayUserFromContext(r.Context())
	if user == nil {
		slog.Warn("gateway chat: unauthorized", "ua", r.UserAgent(), "ct", r.Header.Get("Content-Type"))
		middleware.GatewayError(w, "unauthorized", "authentication_error", "invalid_api_key", http.StatusUnauthorized)
		return
	}
	projectID := middleware.GatewayProjectIDFromContext(r.Context())

	var req requests.GatewayChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		slog.Warn("gateway chat: invalid request body",
			"error", err,
			"content_type", r.Header.Get("Content-Type"),
			"content_length", r.ContentLength,
			"ua", r.UserAgent(),
			"user_id", user.ID, "project_id", projectID)
		middleware.GatewayError(w, "invalid request body: "+err.Error(), "invalid_request_error", "invalid_body", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		slog.Warn("gateway chat: missing model",
			"ua", r.UserAgent(), "user_id", user.ID, "project_id", projectID)
		middleware.GatewayError(w, "model is required", "invalid_request_error", "missing_model", http.StatusBadRequest)
		return
	}

	// Validate model is in user's allowed list
	allowed := v.Controller.GatewayAllowedModels(user)
	modelAllowed := false
	for _, m := range allowed {
		if m == req.Model {
			modelAllowed = true
			break
		}
	}
	if !modelAllowed {
		slog.Warn("gateway chat: model not on plan",
			"model", req.Model, "allowed", allowed,
			"user_id", user.ID, "project_id", projectID)
		middleware.GatewayError(w, fmt.Sprintf("model %q is not available on your plan", req.Model), "invalid_request_error", "model_not_allowed", http.StatusForbidden)
		return
	}

	// Reject image models on the chat endpoint
	if strings.HasPrefix(req.Model, "fal-ai/") {
		slog.Warn("gateway chat: image model on chat endpoint",
			"model", req.Model, "user_id", user.ID, "project_id", projectID)
		middleware.GatewayError(w, "image models must use the /v2/gateway/images/generate endpoint", "invalid_request_error", "wrong_endpoint", http.StatusBadRequest)
		return
	}

	// Anthropic's Messages API (which powers claude-* via the OpenAI-compat
	// layer) REQUIRES max_tokens — OpenAI's spec treats it as optional, so
	// an OpenAI-shape client that omits it is valid against the contract
	// but gets 400 from Anthropic. Inject a safe default here so the
	// gateway keeps its "OpenAI-compatible, just works" promise. 4096 fits
	// every Claude 4 family model without requiring beta headers.
	if req.MaxTokens == 0 && strings.HasPrefix(req.Model, "claude-") {
		req.MaxTokens = 4096
	}

	// Pre-check credit balance > overdraft floor
	balance, err := v.Controller.GetOrCreateCreditsBalance(user)
	if err != nil {
		middleware.GatewayError(w, "failed to check credits", "server_error", "credits_error", http.StatusInternalServerError)
		return
	}
	minBalance := v.Controller.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
	if balance.Balance <= minBalance {
		middleware.GatewayError(w, "insufficient AI credits", "insufficient_credits", "credits_depleted", http.StatusPaymentRequired)
		return
	}

	// Resolve upstream provider
	provider := v.Controller.ResolveGatewayProvider(req.Model)

	// Timeout from remote config
	timeoutSec := v.Controller.RemoteConfig.GetInt(remoteconfig.GatewayChatTimeoutSeconds, remoteconfig.Global)
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Re-serialize the request body for upstream
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		middleware.GatewayError(w, "failed to serialize request", "server_error", "marshal_error", http.StatusInternalServerError)
		return
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(bodyBytes))
	if err != nil {
		middleware.GatewayError(w, "failed to create upstream request", "server_error", "upstream_error", http.StatusInternalServerError)
		return
	}
	utils.SetLLMHeaders(upstreamReq, provider)

	upstreamResp, err := gatewayHTTPClient.Do(upstreamReq)
	if err != nil {
		slog.Error("gateway chat: upstream request failed", "error", err, "model", req.Model)
		middleware.GatewayError(w, "upstream provider request failed", "server_error", "upstream_error", http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	// Copy relevant response headers
	for _, h := range []string{"Content-Type", "X-Request-Id"} {
		if val := upstreamResp.Header.Get(h); val != "" {
			w.Header().Set(h, val)
		}
	}

	// Upstream errors are never streamed — they're a single JSON body.
	// Read it entirely, slog the body for diagnosis (this is what every
	// previous "we got 500 from the user app" incident needed but
	// couldn't see), then pass it through to the client verbatim.
	if upstreamResp.StatusCode >= 400 {
		body, readErr := io.ReadAll(upstreamResp.Body)
		if readErr != nil {
			slog.Error("gateway chat: failed to read upstream error body", "error", readErr,
				"status", upstreamResp.StatusCode, "model", req.Model, "provider", provider.Name)
			middleware.GatewayError(w, "failed to read upstream response", "server_error", "upstream_error", http.StatusBadGateway)
			return
		}
		snippet := string(body)
		if len(snippet) > 2048 {
			snippet = snippet[:2048] + "...(truncated)"
		}
		slog.Warn("gateway chat: upstream returned error",
			"status", upstreamResp.StatusCode,
			"model", req.Model,
			"provider", provider.Name,
			"user_id", user.ID, "project_id", projectID,
			"upstream_body", snippet)
		w.WriteHeader(upstreamResp.StatusCode)
		w.Write(body)
		return
	}

	if req.Stream {
		v.handleStreamingChat(w, upstreamResp, user, req.Model, provider.Name, projectID)
	} else {
		v.handleNonStreamingChat(w, upstreamResp, user, req.Model, provider.Name, projectID)
	}
}

// handleStreamingChat proxies a streaming SSE response from the upstream provider.
func (v *HttpViews) handleStreamingChat(w http.ResponseWriter, upstream *http.Response, user *models.User, model, providerName, projectID string) {
	utils.SetSSEHeaders(w)
	w.WriteHeader(upstream.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("gateway chat: ResponseWriter does not support Flusher")
		return
	}

	var lastUsage *utils.UsageInfo
	scanner := bufio.NewScanner(upstream.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Write every line to the client
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

		// Parse data lines for usage information
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}
			var chunk struct {
				Usage *utils.UsageInfo `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil && chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
		}
	}

	// Post-hoc credit deduction and usage recording
	if lastUsage != nil {
		v.recordGatewayChatUsage(user, model, providerName, projectID, lastUsage)
	}
}

// handleNonStreamingChat proxies a non-streaming response from the upstream provider.
func (v *HttpViews) handleNonStreamingChat(w http.ResponseWriter, upstream *http.Response, user *models.User, model, providerName, projectID string) {
	body, err := io.ReadAll(upstream.Body)
	if err != nil {
		slog.Error("gateway chat: failed to read upstream response", "error", err)
		middleware.GatewayError(w, "failed to read upstream response", "server_error", "upstream_error", http.StatusBadGateway)
		return
	}

	w.WriteHeader(upstream.StatusCode)
	w.Write(body)

	// Parse usage from response for credit deduction
	if upstream.StatusCode == http.StatusOK {
		var resp struct {
			Usage *utils.UsageInfo `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.Usage != nil {
			v.recordGatewayChatUsage(user, model, providerName, projectID, resp.Usage)
		}
	}
}

// recordGatewayChatUsage deducts credits and records token usage after a chat completion.
func (v *HttpViews) recordGatewayChatUsage(user *models.User, model, providerName, projectID string, usage *utils.UsageInfo) {
	credits := v.Controller.TokensToCredits(usage.PromptTokens, usage.CompletionTokens)
	if credits > 0 {
		if err := v.Controller.DeductAICreditsToFloor(user, credits, "gateway_chat", projectID); err != nil {
			slog.Error("gateway chat: failed to deduct credits", "error", err, "user_id", user.ID, "credits", credits)
		}
	}

	tracker := utils.NewTokenTracker()
	tracker.Record(model, *usage, 0, true)
	go v.Controller.RecordTokenUsage(user, "gateway_chat", model, providerName, projectID, "gateway", tracker)
}

// GatewayImage handles POST /v2/gateway/images/generate.
// Generates images using fal.ai models and deducts credits.
func (v *HttpViews) GatewayImage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GatewayUserFromContext(r.Context())
	if user == nil {
		middleware.GatewayError(w, "unauthorized", "authentication_error", "invalid_api_key", http.StatusUnauthorized)
		return
	}
	projectID := middleware.GatewayProjectIDFromContext(r.Context())

	var req requests.GatewayImageRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		middleware.GatewayError(w, "invalid request body: "+err.Error(), "invalid_request_error", "invalid_body", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		middleware.GatewayError(w, "model is required", "invalid_request_error", "missing_model", http.StatusBadRequest)
		return
	}

	// Validate model is in user's allowed list
	allowed := v.Controller.GatewayAllowedModels(user)
	modelAllowed := false
	for _, m := range allowed {
		if m == req.Model {
			modelAllowed = true
			break
		}
	}
	if !modelAllowed {
		middleware.GatewayError(w, fmt.Sprintf("model %q is not available on your plan", req.Model), "invalid_request_error", "model_not_allowed", http.StatusForbidden)
		return
	}

	// Image models must have fal-ai/ prefix
	if !strings.HasPrefix(req.Model, "fal-ai/") {
		middleware.GatewayError(w, "chat models must use the /v2/gateway/chat/completions endpoint", "invalid_request_error", "wrong_endpoint", http.StatusBadRequest)
		return
	}

	// Validate num_images
	numImages := req.NumImages
	if numImages <= 0 {
		numImages = 1
	}
	if numImages > constants.GatewayMaxImagesPerRequest {
		middleware.GatewayError(w, fmt.Sprintf("num_images exceeds maximum of %d per request.", constants.GatewayMaxImagesPerRequest), "invalid_request_error", "invalid_request", http.StatusBadRequest)
		return
	}

	// Calculate total cost
	costPerImage := v.Controller.GatewayImageCreditCost(req.Model)
	totalCost := costPerImage * numImages

	// Check balance >= total cost
	balance, err := v.Controller.GetOrCreateCreditsBalance(user)
	if err != nil {
		middleware.GatewayError(w, "failed to check credits", "server_error", "credits_error", http.StatusInternalServerError)
		return
	}
	if balance.Balance < totalCost {
		middleware.GatewayError(w,
			fmt.Sprintf("insufficient credits: need %d, have %d", totalCost, balance.Balance),
			"insufficient_credits", "credits_depleted", http.StatusPaymentRequired)
		return
	}

	// Pre-deduct credits
	if err := v.Controller.DeductAICreditsUnchecked(user, totalCost, "gateway_image", projectID); err != nil {
		slog.Error("gateway image: failed to pre-deduct credits", "error", err, "user_id", user.ID)
		middleware.GatewayError(w, "failed to deduct credits", "server_error", "credits_error", http.StatusInternalServerError)
		return
	}

	// Timeout from remote config
	timeoutSec := v.Controller.RemoteConfig.GetInt(remoteconfig.GatewayImageTimeoutSeconds, remoteconfig.Global)
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	apiKey := v.Controller.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		// Refund all credits
		v.Controller.AddAICredits(user, totalCost, "gateway_image_refund", projectID)
		middleware.GatewayError(w, "image generation service not configured", "server_error", "service_unavailable", http.StatusServiceUnavailable)
		return
	}

	type imageResult struct {
		URL string `json:"url"`
	}
	var images []imageResult
	generated := 0

	for i := 0; i < numImages; i++ {
		url, err := fal.GenerateImage(ctx, apiKey, req.Model, req.Prompt, req.ImageSize)
		if err != nil {
			slog.Error("gateway image: generation failed", "error", err, "model", req.Model, "image_index", i)
			break
		}
		images = append(images, imageResult{URL: url})
		generated++
	}

	// Refund credits for images that failed to generate
	failedCount := numImages - generated
	if failedCount > 0 {
		refund := costPerImage * failedCount
		if err := v.Controller.AddAICredits(user, refund, "gateway_image_refund", projectID); err != nil {
			slog.Error("gateway image: failed to refund credits", "error", err, "user_id", user.ID, "refund", refund)
		}
	}

	if generated == 0 {
		middleware.GatewayError(w, "all image generations failed", "server_error", "generation_failed", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"images": images,
	}, http.StatusOK)
}

// GetGatewayKeyInfo returns the gateway key prefix for a project (dashboard use).
func (v *HttpViews) GetGatewayKeyInfo(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	projectID := chi.URLParam(r, "appId")

	project, err := v.Controller.GetAppProject(projectID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	gwKey, err := v.Controller.DB.GetProjectGatewayKeyByProjectID(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, "No gateway key found for this project.", http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"key_prefix": gwKey.KeyPrefix,
	}, http.StatusOK)
}

// RevealGatewayKey decrypts and returns the full gateway key.
func (v *HttpViews) RevealGatewayKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	projectID := chi.URLParam(r, "appId")

	project, err := v.Controller.GetAppProject(projectID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	rawKey, err := v.Controller.GetDecryptedGatewayKey(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, "Failed to decrypt gateway key.", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"key": rawKey,
	}, http.StatusOK)
}

// RotateGatewayKey rotates the gateway key for a project.
func (v *HttpViews) RotateGatewayKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	projectID := chi.URLParam(r, "appId")

	project, err := v.Controller.GetAppProject(projectID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	newKey, err := v.Controller.RotateProjectGatewayKey(project)
	if err != nil {
		utils.JSONErrorResponse(w, "Failed to rotate gateway key.", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"key": newKey,
	}, http.StatusCreated)
}
