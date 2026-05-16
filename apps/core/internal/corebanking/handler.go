package corebanking

import (
	"encoding/json"
	"errors"
	"lenz-core/apps/auth/authn"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var _ ServerInterface = (*HTTPServer)(nil)

type HTTPServer struct {
	service    *Service
	demoRoutes bool
}

type HandlerOption func(*HTTPServer)

func WithDemoRoutes(enabled bool) HandlerOption {
	return func(h *HTTPServer) {
		h.demoRoutes = enabled
	}
}

func NewHandler(service *Service, options ...HandlerOption) *HTTPServer {
	h := &HTTPServer{service: service}
	for _, option := range options {
		option(h)
	}
	return h
}

func (h *HTTPServer) Routes(r chi.Router) {
	HandlerWithOptions(h, ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: openAPIRequestError,
	})
}

func openAPIRequestError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("bad_request request_id=%s method=%s path=%s error=%v", requestID(r), r.Method, r.URL.Path, err)
	writeError(w, http.StatusBadRequest, "invalid_request")
}

func (h *HTTPServer) SeedDemo(w http.ResponseWriter, r *http.Request) {
	if !h.demoRoutes {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := institutionScopeString(r, DemoInstitutionID); err != nil {
		respond(w, r, nil, err)
		return
	}
	result, err := h.service.SeedDemo(r.Context())
	respond(w, r, result, err)
}

func (h *HTTPServer) ListCustomerAccounts(w http.ResponseWriter, r *http.Request, customerID openapi_types.UUID, params ListCustomerAccountsParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	accounts, err := h.service.ListCustomerAccounts(r.Context(), institutionID, customerID.String())
	respond(w, r, accounts, err)
}

func (h *HTTPServer) GetAccountBalance(w http.ResponseWriter, r *http.Request, accountID openapi_types.UUID, params GetAccountBalanceParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	balance, err := h.service.GetBalance(r.Context(), institutionID, accountID.String())
	respond(w, r, balance, err)
}

func (h *HTTPServer) ListAccountTransactions(w http.ResponseWriter, r *http.Request, accountID openapi_types.UUID, params ListAccountTransactionsParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	txns, err := h.service.GetTransactions(r.Context(), institutionID, accountID.String(), listTransactionsOptions(params))
	respond(w, r, txns, err)
}

func (h *HTTPServer) MockInboundTransfer(w http.ResponseWriter, r *http.Request, params MockInboundTransferParams) {
	if !h.demoRoutes {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	req, err := bindMockTransferRequest(r, params.IdempotencyKey)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	transfer, err := h.service.MockInbound(r.Context(), req)
	respond(w, r, transfer, err)
}

func (h *HTTPServer) MockOutboundTransfer(w http.ResponseWriter, r *http.Request, params MockOutboundTransferParams) {
	if !h.demoRoutes {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	req, err := bindMockTransferRequest(r, params.IdempotencyKey)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	transfer, err := h.service.MockOutbound(r.Context(), req)
	respond(w, r, transfer, err)
}

func (h *HTTPServer) GetTransfer(w http.ResponseWriter, r *http.Request, transferID openapi_types.UUID, params GetTransferParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	transfer, err := h.service.GetTransfer(r.Context(), institutionID, transferID.String())
	respond(w, r, transfer, err)
}

func (h *HTTPServer) ReverseTransfer(w http.ResponseWriter, r *http.Request, transferID openapi_types.UUID, params ReverseTransferParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	transfer, err := h.service.ReverseTransfer(r.Context(), institutionID, transferID.String(), params.IdempotencyKey)
	respond(w, r, transfer, err)
}

func (h *HTTPServer) GetJournal(w http.ResponseWriter, r *http.Request, journalEntryID openapi_types.UUID, params GetJournalParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	journal, err := h.service.GetJournal(r.Context(), institutionID, journalEntryID.String())
	respond(w, r, journal, err)
}

func (h *HTTPServer) ListTransfers(w http.ResponseWriter, r *http.Request, params ListTransfersParams) {
	institutionID, err := institutionScope(r, params.XInstitutionID)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	transfers, err := h.service.ListTransfers(r.Context(), institutionID)
	respond(w, r, transfers, err)
}

func (req *MockTransferRequest) Bind(r *http.Request) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.AccountId.String() == "00000000-0000-0000-0000-000000000000" || req.AmountMinor <= 0 {
		return ErrInvalidRequest
	}
	if req.Status != nil && !validMockTransferStatus(*req.Status) {
		return ErrInvalidRequest
	}
	if req.Scenario != nil && !validMockTransferScenario(*req.Scenario) {
		return ErrInvalidRequest
	}
	return nil
}

func validMockTransferStatus(status MockTransferRequestStatus) bool {
	switch status {
	case MockTransferRequestStatusPending, MockTransferRequestStatusSucceeded, MockTransferRequestStatusFailed:
		return true
	default:
		return false
	}
}

func validMockTransferScenario(scenario MockTransferRequestScenario) bool {
	switch scenario {
	case MockTransferRequestScenarioSuccess,
		MockTransferRequestScenarioPending,
		MockTransferRequestScenarioFailed,
		MockTransferRequestScenarioDuplicate,
		MockTransferRequestScenarioDelayed,
		MockTransferRequestScenarioReversal:
		return true
	default:
		return false
	}
}

func bindMockTransferRequest(r *http.Request, headerIdempotencyKey string) (TransferRequest, error) {
	var body MockTransferRequest
	if err := render.Bind(r, &body); err != nil {
		return TransferRequest{}, ErrInvalidRequest
	}
	req := TransferRequest{
		InstitutionID:     optionalUUIDString(body.InstitutionId),
		AccountID:         body.AccountId.String(),
		AmountMinor:       body.AmountMinor,
		CurrencyID:        optionalString(body.CurrencyId),
		IdempotencyKey:    firstNonBlank(optionalString(body.IdempotencyKey), headerIdempotencyKey),
		ProviderEventID:   optionalString(body.ProviderEventId),
		ProviderReference: optionalString(body.ProviderReference),
		Status:            optionalEnumString(body.Status),
		Narration:         optionalString(body.Narration),
		Scenario:          optionalEnumString(body.Scenario),
		DelaySeconds:      optionalInt64(body.DelaySeconds),
	}
	if err := applyRequestScope(r, &req); err != nil {
		return TransferRequest{}, err
	}
	return req, nil
}

func applyRequestScope(r *http.Request, req *TransferRequest) error {
	institutionID, err := institutionScopeString(r, institutionID(r))
	if err != nil {
		return err
	}
	if req.InstitutionID != "" && strings.TrimSpace(req.InstitutionID) != institutionID {
		return ErrForbidden
	}
	req.InstitutionID = institutionID
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = idempotencyKey(r)
	}
	return nil
}

func institutionScope(r *http.Request, headerInstitutionID *InstitutionID) (string, error) {
	header := ""
	if headerInstitutionID != nil {
		header = headerInstitutionID.String()
	}
	return institutionScopeString(r, header)
}

func institutionScopeString(r *http.Request, headerInstitutionID string) (string, error) {
	principal, ok := authn.PrincipalFromRequest(r)
	if !ok {
		return "", ErrUnauthorized
	}
	institutionID := strings.TrimSpace(principal.InstitutionID)
	headerInstitutionID = strings.TrimSpace(headerInstitutionID)
	if institutionID == "" {
		return "", ErrUnauthorized
	}
	if headerInstitutionID != "" && headerInstitutionID != institutionID {
		return "", ErrForbidden
	}
	return institutionID, nil
}

func institutionID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Institution-ID"))
}

func idempotencyKey(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalEnumString[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func optionalUUIDString(value *openapi_types.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func optionalInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func listTransactionsOptions(params ListAccountTransactionsParams) ListTransactionsOptions {
	options := ListTransactionsOptions{}
	if params.Limit != nil {
		options.Limit = *params.Limit
	}
	if params.BeforeCreatedAt != nil {
		before := *params.BeforeCreatedAt
		options.BeforeCreatedAt = &before
	}
	return options
}

func respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	switch {
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrInsufficient):
		writeError(w, http.StatusUnprocessableEntity, "insufficient_funds")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeInternalError(w, r, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	requestID := requestID(r)
	log.Printf("internal_error request_id=%s method=%s path=%s error=%v", requestID, r.Method, r.URL.Path, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{
		"message":    "internal_server_error",
		"request_id": requestID,
	})
}

func requestID(r *http.Request) string {
	if requestID := strings.TrimSpace(middleware.GetReqID(r.Context())); requestID != "" {
		return requestID
	}
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	return uuid.Must(uuid.NewRandom()).String()
}
