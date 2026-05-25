package corebanking

import (
	"context"
	"net/http"
	"strings"
)

func (h *HTTPServer) ListReconciliationItems(ctx context.Context, request ListReconciliationItemsRequestObject) (ListReconciliationItemsResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	items, err := h.service.ListReconciliationItems(ctx, institutionID, listReconciliationItemsOptions(request.Params))
	if err != nil {
		return nil, err
	}
	return okResponse(items), nil
}

func (h *HTTPServer) GetReconciliationItem(ctx context.Context, request GetReconciliationItemRequestObject) (GetReconciliationItemResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	item, err := h.service.GetReconciliationItem(ctx, institutionID, request.TransferId.String())
	if err != nil {
		return nil, err
	}
	return okResponse(item), nil
}

func (h *HTTPServer) MarkReconciliationItemReviewed(ctx context.Context, request MarkReconciliationItemReviewedRequestObject) (MarkReconciliationItemReviewedResponseObject, error) {
	institutionID, err := institutionScope(ctx, request.Params.XInstitutionID)
	if err != nil {
		return nil, err
	}
	input, err := bindMarkReconciliationItemReviewedRequest(institutionID, request.TransferId.String(), request.Body)
	if err != nil {
		return nil, err
	}
	item, err := h.service.MarkReconciliationItemReviewed(ctx, input)
	if err != nil {
		return nil, err
	}
	return okResponse(item), nil
}

func (req *MarkReconciliationItemReviewedRequest) Bind(r *http.Request) error {
	return validateMarkReconciliationItemReviewedRequest(req)
}

func validateMarkReconciliationItemReviewedRequest(req *MarkReconciliationItemReviewedRequest) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if strings.TrimSpace(req.ResolutionNote) == "" || !validReconciliationReviewStatus(string(req.ResolutionStatus)) {
		return ErrInvalidRequest
	}
	return nil
}

func bindMarkReconciliationItemReviewedRequest(institutionID, transferID string, body *MarkReconciliationItemReviewedRequest) (MarkReconciliationItemReviewedInput, error) {
	if err := validateMarkReconciliationItemReviewedRequest(body); err != nil {
		return MarkReconciliationItemReviewedInput{}, ErrInvalidRequest
	}
	return MarkReconciliationItemReviewedInput{
		InstitutionID:    institutionID,
		TransferID:       transferID,
		ResolutionNote:   body.ResolutionNote,
		ResolutionStatus: string(body.ResolutionStatus),
	}, nil
}

func listReconciliationItemsOptions(params ListReconciliationItemsParams) ListReconciliationItemsOptions {
	options := ListReconciliationItemsOptions{}
	if params.Limit != nil {
		options.Limit = *params.Limit
	}
	if params.BeforeCreatedAt != nil {
		before := *params.BeforeCreatedAt
		options.BeforeCreatedAt = &before
	}
	if params.BeforeTransferId != nil {
		options.BeforeTransferID = params.BeforeTransferId.String()
	}
	options.Status = optionalEnumString(params.Status)
	options.ProviderStatus = optionalEnumString(params.ProviderStatus)
	options.LedgerStatus = optionalEnumString(params.LedgerStatus)
	options.ReconciliationStatus = optionalEnumString(params.ReconciliationStatus)
	return options
}

func (response strictJSONResponse) VisitListReconciliationItemsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitGetReconciliationItemResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response strictJSONResponse) VisitMarkReconciliationItemReviewedResponse(w http.ResponseWriter) error {
	return response.write(w)
}
