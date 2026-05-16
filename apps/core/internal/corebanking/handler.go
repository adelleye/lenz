package corebanking

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
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
	HandlerFromMux(h, r)
}

func (h *HTTPServer) SeedDemo(w http.ResponseWriter, r *http.Request) {
	if !h.demoRoutes {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	result, err := h.service.SeedDemo(r.Context())
	respond(w, result, err)
}

func (h *HTTPServer) ListCustomerAccounts(w http.ResponseWriter, r *http.Request, customerID openapi_types.UUID, params ListCustomerAccountsParams) {
	accounts, err := h.service.ListCustomerAccounts(r.Context(), params.XInstitutionID.String(), customerID.String())
	respond(w, accounts, err)
}

func (h *HTTPServer) GetAccountBalance(w http.ResponseWriter, r *http.Request, accountID openapi_types.UUID, params GetAccountBalanceParams) {
	balance, err := h.service.GetBalance(r.Context(), params.XInstitutionID.String(), accountID.String())
	respond(w, balance, err)
}

func (h *HTTPServer) ListAccountTransactions(w http.ResponseWriter, r *http.Request, accountID openapi_types.UUID, params ListAccountTransactionsParams) {
	txns, err := h.service.GetTransactions(r.Context(), params.XInstitutionID.String(), accountID.String())
	respond(w, txns, err)
}

func (h *HTTPServer) MockInboundTransfer(w http.ResponseWriter, r *http.Request, params MockInboundTransferParams) {
	if !h.demoRoutes {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	req, err := bindMockTransferRequest(r, params.IdempotencyKey)
	if err != nil {
		respond(w, nil, err)
		return
	}
	transfer, err := h.service.MockInbound(r.Context(), req)
	respond(w, transfer, err)
}

func (h *HTTPServer) MockOutboundTransfer(w http.ResponseWriter, r *http.Request, params MockOutboundTransferParams) {
	if !h.demoRoutes {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	req, err := bindMockTransferRequest(r, params.IdempotencyKey)
	if err != nil {
		respond(w, nil, err)
		return
	}
	transfer, err := h.service.MockOutbound(r.Context(), req)
	respond(w, transfer, err)
}

func (h *HTTPServer) GetTransfer(w http.ResponseWriter, r *http.Request, transferID openapi_types.UUID, params GetTransferParams) {
	transfer, err := h.service.GetTransfer(r.Context(), params.XInstitutionID.String(), transferID.String())
	respond(w, transfer, err)
}

func (h *HTTPServer) ReverseTransfer(w http.ResponseWriter, r *http.Request, transferID openapi_types.UUID, params ReverseTransferParams) {
	transfer, err := h.service.ReverseTransfer(r.Context(), params.XInstitutionID.String(), transferID.String(), params.IdempotencyKey)
	respond(w, transfer, err)
}

func (h *HTTPServer) GetJournal(w http.ResponseWriter, r *http.Request, journalEntryID openapi_types.UUID, params GetJournalParams) {
	journal, err := h.service.GetJournal(r.Context(), params.XInstitutionID.String(), journalEntryID.String())
	respond(w, journal, err)
}

func (h *HTTPServer) ListTransfers(w http.ResponseWriter, r *http.Request, params ListTransfersParams) {
	transfers, err := h.service.ListTransfers(r.Context(), params.XInstitutionID.String())
	respond(w, transfers, err)
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
	headerInstitutionID := institutionID(r)
	if headerInstitutionID == "" {
		return ErrInvalidRequest
	}
	if req.InstitutionID != "" && strings.TrimSpace(req.InstitutionID) != headerInstitutionID {
		return ErrInvalidRequest
	}
	req.InstitutionID = headerInstitutionID
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = idempotencyKey(r)
	}
	return nil
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

func respond(w http.ResponseWriter, v any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrInsufficient):
		writeError(w, http.StatusUnprocessableEntity, "insufficient_funds")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}
