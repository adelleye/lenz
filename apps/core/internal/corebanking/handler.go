package corebanking

import (
	"context"
	"encoding/json"
	"errors"
	"lenz-core/apps/auth/authn"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var _ StrictServerInterface = (*HTTPServer)(nil)

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
	strictHandler := NewStrictHandlerWithOptions(h, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  openAPIRequestError,
		ResponseErrorHandlerFunc: openAPIResponseError,
	})
	HandlerWithOptions(strictHandler, ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: openAPIRequestError,
	})
}

func openAPIRequestError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("bad_request request_id=%s method=%s path=%s error=%v", requestID(r), r.Method, r.URL.Path, err)
	writeError(w, http.StatusBadRequest, "invalid_request")
}

func openAPIResponseError(w http.ResponseWriter, r *http.Request, err error) {
	respond(w, r, nil, err)
}

func (h *HTTPServer) SeedDemo(ctx context.Context, request SeedDemoRequestObject) (SeedDemoResponseObject, error) {
	if !h.demoRoutes {
		return nil, ErrNotFound
	}
	if _, err := institutionScopeString(ctx, DemoInstitutionID); err != nil {
		return nil, err
	}
	result, err := h.service.SeedDemo(ctx)
	if err != nil {
		return nil, err
	}
	return okResponse(result), nil
}

func (h *HTTPServer) CreateCustomer(ctx context.Context, request CreateCustomerRequestObject) (CreateCustomerResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindCreateCustomerRequest(institutionID, request.Body)
	if err != nil {
		return nil, err
	}
	customer, err := h.service.CreateCustomer(ctx, input)
	if err != nil {
		return nil, err
	}
	return createdResponse(customer), nil
}

func (h *HTTPServer) GetCustomer(ctx context.Context, request GetCustomerRequestObject) (GetCustomerResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	customer, err := h.service.GetCustomer(ctx, institutionID, request.CustomerId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(customer), nil
}

func (h *HTTPServer) CreateAccount(ctx context.Context, request CreateAccountRequestObject) (CreateAccountResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindCreateAccountRequest(institutionID, request.Body)
	if err != nil {
		return nil, err
	}
	account, err := h.service.CreateAccount(ctx, input)
	if err != nil {
		return nil, err
	}
	return createdResponse(account), nil
}

func (h *HTTPServer) GetAccount(ctx context.Context, request GetAccountRequestObject) (GetAccountResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	account, err := h.service.GetAccount(ctx, institutionID, request.AccountId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(account), nil
}

func (h *HTTPServer) ListCustomerAccounts(ctx context.Context, request ListCustomerAccountsRequestObject) (ListCustomerAccountsResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	accounts, err := h.service.ListCustomerAccounts(ctx, institutionID, request.CustomerId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(accounts), nil
}

func (h *HTTPServer) GetAccountBalance(ctx context.Context, request GetAccountBalanceRequestObject) (GetAccountBalanceResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	balance, err := h.service.GetBalance(ctx, institutionID, request.AccountId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(balance), nil
}

func (h *HTTPServer) CreateInternalCredit(ctx context.Context, request CreateInternalCreditRequestObject) (CreateInternalCreditResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindInternalCreditRequest(institutionID, request.Body)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.InternalCredit(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) CreateInternalDebit(ctx context.Context, request CreateInternalDebitRequestObject) (CreateInternalDebitResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindInternalDebitRequest(institutionID, request.Body)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.InternalDebit(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) CreateInternalTransfer(ctx context.Context, request CreateInternalTransferRequestObject) (CreateInternalTransferResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindInternalTransferRequest(institutionID, request.Body)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.InternalTransfer(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) ListAccountTransactions(ctx context.Context, request ListAccountTransactionsRequestObject) (ListAccountTransactionsResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	txns, err := h.service.GetTransactions(ctx, institutionID, request.AccountId.String(), listTransactionsOptions(request.Params))
	if err != nil {
		return nil, err
	}
	return okResponse(txns), nil
}

func (h *HTTPServer) MockInboundTransfer(ctx context.Context, request MockInboundTransferRequestObject) (MockInboundTransferResponseObject, error) {
	if !h.demoRoutes {
		return nil, ErrNotFound
	}
	req, err := bindMockTransferRequest(ctx, request.Body, request.Params.XInstitutionID, request.Params.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.MockInbound(ctx, req)
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) MockOutboundTransfer(ctx context.Context, request MockOutboundTransferRequestObject) (MockOutboundTransferResponseObject, error) {
	if !h.demoRoutes {
		return nil, ErrNotFound
	}
	req, err := bindMockTransferRequest(ctx, request.Body, request.Params.XInstitutionID, request.Params.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.MockOutbound(ctx, req)
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) GetTransfer(ctx context.Context, request GetTransferRequestObject) (GetTransferResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.GetTransfer(ctx, institutionID, request.TransferId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) ReverseTransfer(ctx context.Context, request ReverseTransferRequestObject) (ReverseTransferResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	transfer, err := h.service.ReverseTransfer(ctx, institutionID, request.TransferId.String(), request.Params.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	return okResponse(transfer), nil
}

func (h *HTTPServer) GetJournal(ctx context.Context, request GetJournalRequestObject) (GetJournalResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	journal, err := h.service.GetJournal(ctx, institutionID, request.JournalEntryId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(journal), nil
}

func (h *HTTPServer) ListTransfers(ctx context.Context, request ListTransfersRequestObject) (ListTransfersResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	transfers, err := h.service.ListTransfers(ctx, institutionID)
	if err != nil {
		return nil, err
	}
	return okResponse(transfers), nil
}

func (req *CreateCustomerRequest) Bind(r *http.Request) error {
	return validateCreateCustomerRequest(req)
}

func validateCreateCustomerRequest(req *CreateCustomerRequest) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.BranchId.String() == "00000000-0000-0000-0000-000000000000" || strings.TrimSpace(string(req.CustomerType)) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *CreateAccountRequest) Bind(r *http.Request) error {
	return validateCreateAccountRequest(req)
}

func validateCreateAccountRequest(req *CreateAccountRequest) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.CustomerId.String() == "00000000-0000-0000-0000-000000000000" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.AccountNumber) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *InternalCreditRequest) Bind(r *http.Request) error {
	return validateInternalCreditRequest(req)
}

func validateInternalCreditRequest(req *InternalCreditRequest) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.AccountId.String() == "00000000-0000-0000-0000-000000000000" || req.AmountMinor <= 0 || string(req.CurrencyId) != "NGN" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *InternalDebitRequest) Bind(r *http.Request) error {
	return validateInternalDebitRequest(req)
}

func validateInternalDebitRequest(req *InternalDebitRequest) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.AccountId.String() == "00000000-0000-0000-0000-000000000000" || req.AmountMinor <= 0 || string(req.CurrencyId) != "NGN" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *InternalTransferRequest) Bind(r *http.Request) error {
	return validateInternalTransferRequest(req)
}

func validateInternalTransferRequest(req *InternalTransferRequest) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.SourceAccountId.String() == "00000000-0000-0000-0000-000000000000" ||
		req.DestinationAccountId.String() == "00000000-0000-0000-0000-000000000000" ||
		req.SourceAccountId == req.DestinationAccountId ||
		req.AmountMinor <= 0 ||
		string(req.CurrencyId) != "NGN" ||
		strings.TrimSpace(req.IdempotencyKey) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *MockTransferRequest) Bind(r *http.Request) error {
	return validateMockTransferRequest(req)
}

func validateMockTransferRequest(req *MockTransferRequest) error {
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

func bindMockTransferRequest(ctx context.Context, body *MockTransferRequest, headerInstitutionID *InstitutionID, headerIdempotencyKey string) (TransferRequest, error) {
	if body == nil {
		return TransferRequest{}, ErrInvalidRequest
	}
	if err := validateMockTransferRequest(body); err != nil {
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
	if err := applyRequestScope(ctx, headerInstitutionID, headerIdempotencyKey, &req); err != nil {
		return TransferRequest{}, err
	}
	return req, nil
}

func bindCreateCustomerRequest(institutionID string, body *CreateCustomerRequest) (CreateCustomerInput, error) {
	if err := validateCreateCustomerRequest(body); err != nil {
		return CreateCustomerInput{}, ErrInvalidRequest
	}
	return CreateCustomerInput{
		InstitutionID: institutionID,
		BranchID:      body.BranchId.String(),
		CustomerType:  string(body.CustomerType),
		FirstName:     optionalString(body.FirstName),
		LastName:      optionalString(body.LastName),
		BusinessName:  optionalString(body.BusinessName),
		Email:         optionalEmailString(body.Email),
		Phone:         optionalString(body.Phone),
	}, nil
}

func bindCreateAccountRequest(institutionID string, body *CreateAccountRequest) (CreateAccountInput, error) {
	if err := validateCreateAccountRequest(body); err != nil {
		return CreateAccountInput{}, ErrInvalidRequest
	}
	return CreateAccountInput{
		InstitutionID:        institutionID,
		CustomerID:           body.CustomerId.String(),
		AccountNumber:        body.AccountNumber,
		Name:                 body.Name,
		ProductType:          optionalEnumString(body.ProductType),
		CurrencyID:           optionalString(body.CurrencyId),
		AllowNegativeBalance: optionalBool(body.AllowNegativeBalance),
	}, nil
}

func bindInternalCreditRequest(institutionID string, body *InternalCreditRequest) (InternalCreditInput, error) {
	if err := validateInternalCreditRequest(body); err != nil {
		return InternalCreditInput{}, ErrInvalidRequest
	}
	return InternalCreditInput{
		InstitutionID:   institutionID,
		AccountID:       body.AccountId.String(),
		SourceAccountID: optionalUUIDString(body.SourceAccountId),
		AmountMinor:     body.AmountMinor,
		CurrencyID:      string(body.CurrencyId),
		IdempotencyKey:  body.IdempotencyKey,
		Narration:       optionalString(body.Narration),
		Reference:       optionalString(body.Reference),
	}, nil
}

func bindInternalDebitRequest(institutionID string, body *InternalDebitRequest) (InternalDebitInput, error) {
	if err := validateInternalDebitRequest(body); err != nil {
		return InternalDebitInput{}, ErrInvalidRequest
	}
	return InternalDebitInput{
		InstitutionID:        institutionID,
		AccountID:            body.AccountId.String(),
		DestinationAccountID: optionalUUIDString(body.DestinationAccountId),
		AmountMinor:          body.AmountMinor,
		CurrencyID:           string(body.CurrencyId),
		IdempotencyKey:       body.IdempotencyKey,
		Narration:            optionalString(body.Narration),
		Reference:            optionalString(body.Reference),
	}, nil
}

func bindInternalTransferRequest(institutionID string, body *InternalTransferRequest) (InternalTransferInput, error) {
	if err := validateInternalTransferRequest(body); err != nil {
		return InternalTransferInput{}, ErrInvalidRequest
	}
	return InternalTransferInput{
		InstitutionID:        institutionID,
		SourceAccountID:      body.SourceAccountId.String(),
		DestinationAccountID: body.DestinationAccountId.String(),
		AmountMinor:          body.AmountMinor,
		CurrencyID:           string(body.CurrencyId),
		IdempotencyKey:       body.IdempotencyKey,
		Narration:            optionalString(body.Narration),
		Reference:            optionalString(body.Reference),
	}, nil
}

func applyRequestScope(ctx context.Context, headerInstitutionID *InstitutionID, headerIdempotencyKey string, req *TransferRequest) error {
	institutionID, err := institutionScope(ctx, headerInstitutionID)
	if err != nil {
		return err
	}
	if req.InstitutionID != "" && strings.TrimSpace(req.InstitutionID) != institutionID {
		return ErrForbidden
	}
	req.InstitutionID = institutionID
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = strings.TrimSpace(headerIdempotencyKey)
	}
	return nil
}

func institutionScope(ctx context.Context, headerInstitutionID *InstitutionID) (string, error) {
	header := ""
	if headerInstitutionID != nil {
		header = headerInstitutionID.String()
	}
	return institutionScopeString(ctx, header)
}

func institutionScopeString(ctx context.Context, headerInstitutionID string) (string, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
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

func optionalBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func optionalUUIDString(value *openapi_types.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func optionalEmailString(value *openapi_types.Email) string {
	if value == nil {
		return ""
	}
	return string(*value)
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

type strictJSONResponse struct {
	status int
	body   any
}

func okResponse(body any) strictJSONResponse {
	return strictJSONResponse{status: http.StatusOK, body: body}
}

func createdResponse(body any) strictJSONResponse {
	return strictJSONResponse{status: http.StatusCreated, body: body}
}

func (response strictJSONResponse) write(w http.ResponseWriter) error {
	return writeJSONResponse(w, response.status, response.body)
}

func (response strictJSONResponse) VisitCreateCustomerResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitGetCustomerResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitCreateAccountResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitGetAccountResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitGetAccountBalanceResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitCreateInternalCreditResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitCreateInternalDebitResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitCreateInternalTransferResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitListAccountTransactionsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitGetJournalResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitListTransfersResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitListCustomerAccountsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitSeedDemoResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitMockInboundTransferResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitMockOutboundTransferResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitGetTransferResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitReverseTransferResponse(w http.ResponseWriter) error {
	return response.write(w)
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
	_ = writeJSONResponse(w, status, v)
}

func writeJSONResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
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
