package corebanking

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	NameEnquiryStatusFound               = "found"
	NameEnquiryStatusNotFound            = "not_found"
	NameEnquiryStatusProviderUnavailable = "provider_unavailable"
)

type ExternalNameEnquiryInput struct {
	InstitutionID              string
	Provider                   string
	DestinationInstitutionCode string
	AccountNumber              string
	CurrencyID                 string
}

type ExternalNameEnquiryResult struct {
	Provider                   string    `json:"provider"`
	DestinationInstitutionCode string    `json:"destination_institution_code"`
	AccountNumber              string    `json:"account_number"`
	AccountName                string    `json:"account_name"`
	ProviderReference          *string   `json:"provider_reference,omitempty"`
	Status                     string    `json:"status"`
	Message                    string    `json:"message"`
	CreatedAt                  time.Time `json:"created_at"`
}

func (s *Service) ExternalNameEnquiry(ctx context.Context, input ExternalNameEnquiryInput) (*ExternalNameEnquiryResult, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	providerName := strings.TrimSpace(input.Provider)
	if providerName == "" {
		providerName = ProviderMockNIP
	}
	destinationInstitutionCode := strings.TrimSpace(input.DestinationInstitutionCode)
	accountNumber := strings.TrimSpace(input.AccountNumber)
	currencyID := strings.ToUpper(strings.TrimSpace(input.CurrencyID))
	if currencyID == "" {
		currencyID = "NGN"
	}
	if destinationInstitutionCode == "" || !isTenDigitAccountNumber(accountNumber) || currencyID != "NGN" {
		return nil, ErrInvalidRequest
	}

	provider, err := s.provider(providerName)
	if err != nil {
		return nil, err
	}
	result, err := provider.NameEnquiry(ctx, NameEnquiryRequest{
		InstitutionID: institutionID,
		BankCode:      destinationInstitutionCode,
		AccountNumber: accountNumber,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidRequest) {
			return nil, ErrInvalidRequest
		}
		return externalNameEnquiryErrorResult(providerName, destinationInstitutionCode, accountNumber, err), nil
	}
	if result == nil || strings.TrimSpace(result.AccountName) == "" {
		return externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, "", nil, NameEnquiryStatusNotFound, "account_not_found"), nil
	}

	providerReference := optionalResultString(result.ProviderReference)
	return externalNameEnquiryResult(
		firstNonBlank(result.Provider, providerName),
		firstNonBlank(result.BankCode, destinationInstitutionCode),
		firstNonBlank(result.AccountNumber, accountNumber),
		strings.TrimSpace(result.AccountName),
		providerReference,
		NameEnquiryStatusFound,
		"account_found",
	), nil
}

func externalNameEnquiryErrorResult(providerName, destinationInstitutionCode, accountNumber string, err error) *ExternalNameEnquiryResult {
	if errors.Is(err, ErrNotFound) {
		return externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, "", nil, NameEnquiryStatusNotFound, "account_not_found")
	}
	return externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, "", nil, NameEnquiryStatusProviderUnavailable, "provider_unavailable")
}

func externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, accountName string, providerReference *string, status, message string) *ExternalNameEnquiryResult {
	return &ExternalNameEnquiryResult{
		Provider:                   providerName,
		DestinationInstitutionCode: destinationInstitutionCode,
		AccountNumber:              accountNumber,
		AccountName:                accountName,
		ProviderReference:          providerReference,
		Status:                     status,
		Message:                    message,
		CreatedAt:                  time.Now().UTC(),
	}
}

func optionalResultString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
