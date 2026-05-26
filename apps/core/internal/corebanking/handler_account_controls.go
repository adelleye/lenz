package corebanking

import (
	"context"
	"net/http"
	"strings"
)

func (h *HTTPServer) FreezeAccount(ctx context.Context, request FreezeAccountRequestObject) (FreezeAccountResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindAccountControlRequest(institutionID, request.AccountId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	account, err := h.service.FreezeAccount(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(account), nil
}

func (h *HTTPServer) UnfreezeAccount(ctx context.Context, request UnfreezeAccountRequestObject) (UnfreezeAccountResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindAccountControlRequest(institutionID, request.AccountId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	account, err := h.service.UnfreezeAccount(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(account), nil
}

func (h *HTTPServer) ActivatePostNoDebit(ctx context.Context, request ActivatePostNoDebitRequestObject) (ActivatePostNoDebitResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindAccountControlRequest(institutionID, request.AccountId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	account, err := h.service.ActivatePostNoDebit(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(account), nil
}

func (h *HTTPServer) DeactivatePostNoDebit(ctx context.Context, request DeactivatePostNoDebitRequestObject) (DeactivatePostNoDebitResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindAccountControlRequest(institutionID, request.AccountId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	account, err := h.service.DeactivatePostNoDebit(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(account), nil
}

func (h *HTTPServer) PlaceAccountLien(ctx context.Context, request PlaceAccountLienRequestObject) (PlaceAccountLienResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindAccountLienRequest(institutionID, request.AccountId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	hold, err := h.service.PlaceAccountLien(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(hold), nil
}

func (h *HTTPServer) ReleaseAccountLien(ctx context.Context, request ReleaseAccountLienRequestObject) (ReleaseAccountLienResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindReleaseLienRequest(institutionID, request.AccountId.String(), request.LienId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	hold, err := h.service.ReleaseAccountLien(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(hold), nil
}

func (req *AccountControlRequest) Bind(r *http.Request) error {
	return validateAccountControlRequest(req)
}

func validateAccountControlRequest(req *AccountControlRequest) error {
	if req == nil || strings.TrimSpace(req.Reference) == "" || strings.TrimSpace(req.Reason) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *AccountLienRequest) Bind(r *http.Request) error {
	return validateAccountLienRequest(req)
}

func validateAccountLienRequest(req *AccountLienRequest) error {
	if req == nil || req.AmountMinor <= 0 || strings.TrimSpace(string(req.CurrencyId)) == "" || strings.TrimSpace(req.Reference) == "" || strings.TrimSpace(req.Reason) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func (req *ReleaseLienRequest) Bind(r *http.Request) error {
	return validateReleaseLienRequest(req)
}

func validateReleaseLienRequest(req *ReleaseLienRequest) error {
	if req == nil || strings.TrimSpace(req.Reference) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func bindAccountControlRequest(institutionID, accountID string, body *AccountControlRequest) (AccountControlInput, error) {
	if err := validateAccountControlRequest(body); err != nil {
		return AccountControlInput{}, ErrInvalidRequest
	}
	return AccountControlInput{
		InstitutionID: institutionID,
		AccountID:     accountID,
		Reference:     body.Reference,
		Reason:        body.Reason,
	}, nil
}

func bindAccountLienRequest(institutionID, accountID string, body *AccountLienRequest) (AccountLienInput, error) {
	if err := validateAccountLienRequest(body); err != nil {
		return AccountLienInput{}, ErrInvalidRequest
	}
	return AccountLienInput{
		InstitutionID: institutionID,
		AccountID:     accountID,
		AmountMinor:   body.AmountMinor,
		CurrencyID:    string(body.CurrencyId),
		Reference:     body.Reference,
		Reason:        body.Reason,
	}, nil
}

func bindReleaseLienRequest(institutionID, accountID, lienID string, body *ReleaseLienRequest) (ReleaseLienInput, error) {
	if err := validateReleaseLienRequest(body); err != nil {
		return ReleaseLienInput{}, ErrInvalidRequest
	}
	return ReleaseLienInput{
		InstitutionID: institutionID,
		AccountID:     accountID,
		LienID:        lienID,
		Reference:     body.Reference,
		Reason:        optionalString(body.Reason),
	}, nil
}

func (response strictJSONResponse) VisitFreezeAccountResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitUnfreezeAccountResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitActivatePostNoDebitResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitDeactivatePostNoDebitResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitPlaceAccountLienResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitReleaseAccountLienResponse(w http.ResponseWriter) error {
	return response.write(w)
}
