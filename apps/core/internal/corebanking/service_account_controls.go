package corebanking

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) FreezeAccount(ctx context.Context, input AccountControlInput) (*Account, error) {
	input, err := validateAccountControlInput(input, true)
	if err != nil {
		return nil, err
	}
	account, err := s.repository.GetAccount(ctx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if !controllableAccount(*account) {
		return nil, ErrInvalidRequest
	}
	return s.repository.SetAccountStatus(ctx, input, AccountStatusFrozen, AccountStatusActive)
}

func (s *Service) UnfreezeAccount(ctx context.Context, input AccountControlInput) (*Account, error) {
	input, err := validateAccountControlInput(input, true)
	if err != nil {
		return nil, err
	}
	account, err := s.repository.GetAccount(ctx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if !controllableAccount(*account) {
		return nil, ErrInvalidRequest
	}
	return s.repository.SetAccountStatus(ctx, input, AccountStatusActive, AccountStatusFrozen)
}

func (s *Service) ActivatePostNoDebit(ctx context.Context, input AccountControlInput) (*Account, error) {
	input, err := validateAccountControlInput(input, true)
	if err != nil {
		return nil, err
	}
	account, err := s.repository.GetAccount(ctx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if !controllableAccount(*account) {
		return nil, ErrInvalidRequest
	}
	return s.repository.SetAccountStatus(ctx, input, AccountStatusPostNoDebit, AccountStatusActive)
}

func (s *Service) DeactivatePostNoDebit(ctx context.Context, input AccountControlInput) (*Account, error) {
	input, err := validateAccountControlInput(input, true)
	if err != nil {
		return nil, err
	}
	account, err := s.repository.GetAccount(ctx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if !controllableAccount(*account) {
		return nil, ErrInvalidRequest
	}
	return s.repository.SetAccountStatus(ctx, input, AccountStatusActive, AccountStatusPostNoDebit)
}

func (s *Service) PlaceAccountLien(ctx context.Context, input AccountLienInput) (*AccountHold, error) {
	var err error
	input.InstitutionID, err = requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.CurrencyID = strings.ToUpper(strings.TrimSpace(input.CurrencyID))
	input.Reference = strings.TrimSpace(input.Reference)
	input.Reason = strings.TrimSpace(input.Reason)
	if _, err := uuid.Parse(input.AccountID); err != nil {
		return nil, ErrInvalidRequest
	}
	if input.AmountMinor <= 0 || input.CurrencyID == "" || input.Reference == "" || input.Reason == "" {
		return nil, ErrInvalidRequest
	}
	return s.repository.PlaceAccountLien(ctx, input)
}

func (s *Service) ReleaseAccountLien(ctx context.Context, input ReleaseLienInput) (*AccountHold, error) {
	var err error
	input.InstitutionID, err = requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.LienID = strings.TrimSpace(input.LienID)
	input.Reference = strings.TrimSpace(input.Reference)
	input.Reason = strings.TrimSpace(input.Reason)
	if _, err := uuid.Parse(input.AccountID); err != nil {
		return nil, ErrInvalidRequest
	}
	if _, err := uuid.Parse(input.LienID); err != nil {
		return nil, ErrInvalidRequest
	}
	if input.Reference == "" {
		return nil, ErrInvalidRequest
	}
	return s.repository.ReleaseAccountLien(ctx, input)
}

func controllableAccount(account Account) bool {
	return account.Kind == AccountKindCustomer && account.Status != AccountStatusClosed
}

func allowedAccountStatusTransition(current string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, status := range allowed {
		if current == status {
			return true
		}
	}
	return false
}

func validateAccountControlInput(input AccountControlInput, requireReason bool) (AccountControlInput, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return input, err
	}
	input.InstitutionID = institutionID
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.Reference = strings.TrimSpace(input.Reference)
	input.Reason = strings.TrimSpace(input.Reason)
	if _, err := uuid.Parse(input.AccountID); err != nil {
		return input, ErrInvalidRequest
	}
	if input.Reference == "" || (requireReason && input.Reason == "") {
		return input, ErrInvalidRequest
	}
	return input, nil
}
