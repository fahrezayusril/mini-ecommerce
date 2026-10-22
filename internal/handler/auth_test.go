package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/service"
)

type fakeAuthService struct {
	registerInputs []service.RegisterInput
	loginInputs    []service.LoginInput
	registerFunc   func(service.RegisterInput) (service.AuthResult, error)
	loginFunc      func(service.LoginInput) (service.AuthResult, error)
}

func (f *fakeAuthService) Register(_ context.Context, input service.RegisterInput) (service.AuthResult, error) {
	f.registerInputs = append(f.registerInputs, input)
	return f.registerFunc(input)
}

func (f *fakeAuthService) Login(_ context.Context, input service.LoginInput) (service.AuthResult, error) {
	f.loginInputs = append(f.loginInputs, input)
	return f.loginFunc(input)
}

var (
	testUserID    = uuid.MustParse("01920000-0000-7000-8000-000000000001")
	testExpiresAt = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
)

func authResult(email string) service.AuthResult {
	return service.AuthResult{
		User:      domain.User{ID: testUserID, Name: "Jane Doe", Email: email, Role: domain.RoleCustomer},
		Token:     "signed.jwt.token",
		ExpiresAt: testExpiresAt,
	}
}

func registeredEmails() func(service.RegisterInput) (service.AuthResult, error) {
	seen := map[string]bool{}
	return func(input service.RegisterInput) (service.AuthResult, error) {
		key := strings.ToLower(input.Email)
		if seen[key] {
			return service.AuthResult{}, domain.ErrEmailAlreadyExists
		}
		seen[key] = true
		return authResult(key), nil
	}
}

func post(handlerFunc http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerFunc(rec, req)
	return rec
}

type errorEnvelope struct {
	Error struct {
		Code    string       `json:"code"`
		Message string       `json:"message"`
		Details []FieldError `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var envelope errorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	return envelope
}

const expectedAuthBody = `{
	"data": {
		"user": {"id": "01920000-0000-7000-8000-000000000001", "name": "Jane Doe", "email": "jane@example.com", "role": "customer"},
		"token": "signed.jwt.token",
		"expires_at": "2026-10-23T10:00:00Z"
	}
}`

func TestRegisterSuccess(t *testing.T) {
	svc := &fakeAuthService{registerFunc: registeredEmails()}
	h := NewAuthHandler(svc)

	rec := post(h.Register, `{"name":"  Jane Doe ","email":" jane@example.com ","password":"supersecret"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, expectedAuthBody, rec.Body.String())
	require.Len(t, svc.registerInputs, 1)
	assert.Equal(t, service.RegisterInput{Name: "Jane Doe", Email: "jane@example.com", Password: "supersecret"}, svc.registerInputs[0])
}

func TestRegisterDuplicateEmailIsCaseInsensitive(t *testing.T) {
	svc := &fakeAuthService{registerFunc: registeredEmails()}
	h := NewAuthHandler(svc)

	first := post(h.Register, `{"name":"Jane","email":"jane@example.com","password":"supersecret"}`)
	require.Equal(t, http.StatusCreated, first.Code)

	second := post(h.Register, `{"name":"Jane","email":"JANE@Example.com","password":"supersecret"}`)

	assert.Equal(t, http.StatusConflict, second.Code)
	envelope := decodeError(t, second)
	assert.Equal(t, "EMAIL_ALREADY_EXISTS", envelope.Error.Code)
	assert.Equal(t, "Email is already registered", envelope.Error.Message)
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantDetails []FieldError
	}{
		{
			name: "all fields missing",
			body: `{}`,
			wantDetails: []FieldError{
				{Field: "name", Message: "is required"},
				{Field: "email", Message: "is required"},
				{Field: "password", Message: "is required"},
			},
		},
		{
			name:        "blank name",
			body:        `{"name":"   ","email":"jane@example.com","password":"supersecret"}`,
			wantDetails: []FieldError{{Field: "name", Message: "is required"}},
		},
		{
			name:        "name too long",
			body:        `{"name":"` + strings.Repeat("a", 101) + `","email":"jane@example.com","password":"supersecret"}`,
			wantDetails: []FieldError{{Field: "name", Message: "must be at most 100 characters"}},
		},
		{
			name:        "invalid email",
			body:        `{"name":"Jane","email":"not-an-email","password":"supersecret"}`,
			wantDetails: []FieldError{{Field: "email", Message: "must be a valid email"}},
		},
		{
			name:        "password too short",
			body:        `{"name":"Jane","email":"jane@example.com","password":"1234567"}`,
			wantDetails: []FieldError{{Field: "password", Message: "must be at least 8 characters"}},
		},
		{
			name:        "password too long",
			body:        `{"name":"Jane","email":"jane@example.com","password":"` + strings.Repeat("p", 73) + `"}`,
			wantDetails: []FieldError{{Field: "password", Message: "must be at most 72 bytes"}},
		},
		{
			name:        "password over 72 bytes with multibyte characters",
			body:        `{"name":"Jane","email":"jane@example.com","password":"` + strings.Repeat("é", 37) + `"}`,
			wantDetails: []FieldError{{Field: "password", Message: "must be at most 72 bytes"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeAuthService{registerFunc: registeredEmails()}
			rec := post(NewAuthHandler(svc).Register, tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
			assert.Equal(t, "Request validation failed", envelope.Error.Message)
			assert.Equal(t, tt.wantDetails, envelope.Error.Details)
			assert.Empty(t, svc.registerInputs)
		})
	}
}

func TestRegisterAcceptsBoundaryLengths(t *testing.T) {
	svc := &fakeAuthService{registerFunc: registeredEmails()}
	body := `{"name":"` + strings.Repeat("a", 100) + `","email":"jane@example.com","password":"` + strings.Repeat("p", 72) + `"}`

	rec := post(NewAuthHandler(svc).Register, body)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestAuthInvalidJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{name: "empty body", body: ``, wantMessage: "Request body must not be empty"},
		{name: "malformed", body: `{"email":`, wantMessage: "Request body contains malformed JSON"},
		{name: "syntax error", body: `{"email" "x"}`, wantMessage: "Request body contains malformed JSON"},
		{name: "array instead of object", body: `[]`, wantMessage: "Request body must be a JSON object"},
		{name: "wrong field type", body: `{"email": 42}`, wantMessage: `Field "email" has an invalid type`},
		{name: "unknown field", body: `{"email":"jane@example.com","password":"supersecret","role":"admin"}`, wantMessage: `Request body contains unknown field "role"`},
		{name: "multiple objects", body: `{"email":"a@b.co","password":"x"}{}`, wantMessage: "Request body must contain a single JSON object"},
		{name: "too large", body: `{"name":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`, wantMessage: "Request body must not exceed 1048576 bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeAuthService{registerFunc: registeredEmails(), loginFunc: func(service.LoginInput) (service.AuthResult, error) {
				return authResult("jane@example.com"), nil
			}}
			h := NewAuthHandler(svc)

			for _, handlerFunc := range []http.HandlerFunc{h.Register, h.Login} {
				rec := post(handlerFunc, tt.body)

				assert.Equal(t, http.StatusBadRequest, rec.Code)
				envelope := decodeError(t, rec)
				assert.Equal(t, "INVALID_JSON", envelope.Error.Code)
				assert.Equal(t, tt.wantMessage, envelope.Error.Message)
			}
			assert.Empty(t, svc.registerInputs)
			assert.Empty(t, svc.loginInputs)
		})
	}
}

func TestRegisterInternalErrorDoesNotLeakDetails(t *testing.T) {
	svc := &fakeAuthService{registerFunc: func(service.RegisterInput) (service.AuthResult, error) {
		return service.AuthResult{}, errors.New("pq: connection refused to 10.0.0.5")
	}}

	rec := post(NewAuthHandler(svc).Register, `{"name":"Jane","email":"jane@example.com","password":"supersecret"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`, rec.Body.String())
}

func TestLoginSuccess(t *testing.T) {
	svc := &fakeAuthService{loginFunc: func(service.LoginInput) (service.AuthResult, error) {
		return authResult("jane@example.com"), nil
	}}

	rec := post(NewAuthHandler(svc).Login, `{"email":" Jane@Example.com ","password":"supersecret"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, expectedAuthBody, rec.Body.String())
	require.Len(t, svc.loginInputs, 1)
	assert.Equal(t, service.LoginInput{Email: "Jane@Example.com", Password: "supersecret"}, svc.loginInputs[0])
}

func TestLoginInvalidCredentialsUseSameResponse(t *testing.T) {
	svc := &fakeAuthService{loginFunc: func(input service.LoginInput) (service.AuthResult, error) {
		return service.AuthResult{}, domain.ErrInvalidCredentials
	}}
	h := NewAuthHandler(svc)

	unknownEmail := post(h.Login, `{"email":"nobody@example.com","password":"supersecret"}`)
	wrongPassword := post(h.Login, `{"email":"jane@example.com","password":"wrongpassword"}`)

	for _, rec := range []*httptest.ResponseRecorder{unknownEmail, wrongPassword} {
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.JSONEq(t, `{"error":{"code":"INVALID_CREDENTIALS","message":"Invalid email or password"}}`, rec.Body.String())
	}
	assert.Equal(t, unknownEmail.Body.String(), wrongPassword.Body.String())
}

func TestLoginValidation(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantDetails []FieldError
	}{
		{
			name: "missing fields",
			body: `{}`,
			wantDetails: []FieldError{
				{Field: "email", Message: "is required"},
				{Field: "password", Message: "is required"},
			},
		},
		{
			name:        "invalid email",
			body:        `{"email":"jane","password":"supersecret"}`,
			wantDetails: []FieldError{{Field: "email", Message: "must be a valid email"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeAuthService{}
			rec := post(NewAuthHandler(svc).Login, tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
			assert.Equal(t, tt.wantDetails, envelope.Error.Details)
			assert.Empty(t, svc.loginInputs)
		})
	}
}

func TestLoginInternalError(t *testing.T) {
	svc := &fakeAuthService{loginFunc: func(service.LoginInput) (service.AuthResult, error) {
		return service.AuthResult{}, errors.New("database is down")
	}}

	rec := post(NewAuthHandler(svc).Login, `{"email":"jane@example.com","password":"supersecret"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`, rec.Body.String())
}
