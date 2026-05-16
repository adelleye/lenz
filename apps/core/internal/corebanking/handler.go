package corebanking

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service    *Service
	demoRoutes bool
}

type HandlerOption func(*Handler)

func WithDemoRoutes(enabled bool) HandlerOption {
	return func(h *Handler) {
		h.demoRoutes = enabled
	}
}

func NewHandler(service *Service, options ...HandlerOption) *Handler {
	h := &Handler{service: service}
	for _, option := range options {
		option(h)
	}
	return h
}

func (h *Handler) Routes(r chi.Router) {
	if h.demoRoutes {
		r.Post("/api/v1/demo/seed", h.seedDemo)
		r.Post("/api/v1/transfers/mock/inbound", h.mockInbound)
		r.Post("/api/v1/transfers/mock/outbound", h.mockOutbound)
	}
	r.Get("/api/v1/customers/{customer_id}/accounts", h.listCustomerAccounts)
	r.Get("/api/v1/accounts/{account_id}/balance", h.getBalance)
	r.Get("/api/v1/accounts/{account_id}/transactions", h.getTransactions)
	r.Get("/api/v1/transfers/{transfer_id}", h.getTransfer)
	r.Post("/api/v1/transfers/{transfer_id}/reverse", h.reverseTransfer)
	r.Get("/api/v1/admin/ledger/journal/{journal_entry_id}", h.getJournal)
	r.Get("/api/v1/admin/transfers", h.listTransfers)
}

func (h *Handler) seedDemo(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.SeedDemo(r.Context())
	respond(w, result, err)
}

func (h *Handler) listCustomerAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.service.ListCustomerAccounts(r.Context(), institutionID(r), chi.URLParam(r, "customer_id"))
	respond(w, accounts, err)
}

func (h *Handler) getBalance(w http.ResponseWriter, r *http.Request) {
	balance, err := h.service.GetBalance(r.Context(), institutionID(r), chi.URLParam(r, "account_id"))
	respond(w, balance, err)
}

func (h *Handler) getTransactions(w http.ResponseWriter, r *http.Request) {
	txns, err := h.service.GetTransactions(r.Context(), institutionID(r), chi.URLParam(r, "account_id"))
	respond(w, txns, err)
}

func (h *Handler) mockInbound(w http.ResponseWriter, r *http.Request) {
	var req TransferRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := applyRequestScope(r, &req); err != nil {
		respond(w, nil, err)
		return
	}
	transfer, err := h.service.MockInbound(r.Context(), req)
	respond(w, transfer, err)
}

func (h *Handler) mockOutbound(w http.ResponseWriter, r *http.Request) {
	var req TransferRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := applyRequestScope(r, &req); err != nil {
		respond(w, nil, err)
		return
	}
	transfer, err := h.service.MockOutbound(r.Context(), req)
	respond(w, transfer, err)
}

func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, err := h.service.GetTransfer(r.Context(), institutionID(r), chi.URLParam(r, "transfer_id"))
	respond(w, transfer, err)
}

func (h *Handler) reverseTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, err := h.service.ReverseTransfer(r.Context(), institutionID(r), chi.URLParam(r, "transfer_id"), idempotencyKey(r))
	respond(w, transfer, err)
}

func (h *Handler) getJournal(w http.ResponseWriter, r *http.Request) {
	journal, err := h.service.GetJournal(r.Context(), institutionID(r), chi.URLParam(r, "journal_entry_id"))
	respond(w, journal, err)
}

func (h *Handler) listTransfers(w http.ResponseWriter, r *http.Request) {
	transfers, err := h.service.ListTransfers(r.Context(), institutionID(r))
	respond(w, transfers, err)
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
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
	return r.Header.Get("Idempotency-Key")
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
