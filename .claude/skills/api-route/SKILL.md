---
name: api-route
description: Scaffold a new API endpoint with view handler, controller method, request/response types, and route registration
---

# API Route Generator

Scaffold a new REST API endpoint following the wemob-backend conventions.

## Arguments

- `name`: The resource/action name (e.g., "CreateWidget", "ListNotifications")
- `method`: HTTP method (GET, POST, PUT, DELETE)
- `path`: Route path (e.g., "/v1/widgets", "/v1/widgets/{widgetId}")
- `auth`: Whether the endpoint requires authentication (default: true)
- `subscription`: Whether it requires active subscription (default: false)

## What to Generate

### 1. Request struct (if POST/PUT) in `internal/requests/`

Follow this pattern exactly:

```go
package requests

type <Name>Request struct {
	FieldName string `json:"field_name" validate:"required,min=1,max=100" errormgs:"field name is required"`
}
```

- Always include `json`, `validate`, and `errormgs` tags
- Use go-playground/validator syntax for `validate` tag

### 2. Response struct (if needed) in `internal/responses/`

```go
package responses

type <Name>Response struct {
	Field string `json:"field"`
}
```

### 3. Controller method in `internal/controllers/`

```go
func (c *Controller) <Name>(user *models.User, req requests.CreateSomethingRequest) (*responses.SomethingResponse, error) {
	// Business logic here
	// Use c.DB for database access
	// Use context.Background() for DB context
	// Return (result, nil) or (nil, error)
}
```

- Method on `*Controller`
- First param is `*models.User` if authenticated
- Use `validator.New()` + `utils.ValidateFunc[T]()` for validation if needed in controller
- Return `(value, error)` tuple

### 4. View handler in `internal/views/`

Follow this exact pattern:

```go
func (v *HttpViews) <Name>(w http.ResponseWriter, r *http.Request) {
	// 1. Auth (if required)
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// 2. Subscription check (if required)
	if requireActiveSubscription(w, user) {
		return
	}

	// 3. URL params (if any)
	id := chi.URLParam(r, "paramName")

	// 4. Parse body (if POST/PUT)
	var req requests.SomethingRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 5. Call controller
	result, err := v.Controller.Something(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 6. Respond
	utils.JSONResponse(w, result, http.StatusOK) // or StatusCreated for POST
}
```

### 5. Route registration in `cmd/main.go`

Add the route in the appropriate group:

```go
r.Post("/v1/path", views.HandlerName)
```

## Checklist

- [ ] Request struct has `json` + `validate` + `errormgs` tags
- [ ] View handler follows the 6-step pattern (auth → subscription → params → parse → logic → respond)
- [ ] Controller method uses `*Controller` receiver
- [ ] Route registered in `cmd/main.go`
- [ ] No middleware auth — auth is inline in the view handler via `GetUserFromReq`
