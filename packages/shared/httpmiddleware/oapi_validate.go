package httpmiddleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"lenz-core/packages/shared/utils"
	"net/http"
	"reflect"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"

	"github.com/getkin/kin-openapi/openapi3"
	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
)

// TODO: add custom fields validation github.com/gookit/validate

func requestMiddleware(spec *openapi3.T) strictnethttp.StrictHTTPMiddlewareFunc {
	b, err := spec.MarshalJSON()
	if err != nil {
		// log out error
		return returnMiddleware
	}

	doc, err := libopenapi.NewDocument(b)
	if err != nil {
		return returnMiddleware
	}

	highLevelValidator, validatorErr := validator.NewValidator(doc)
	if len(validatorErr) > 0 {
		return returnMiddleware
	}

	return func(next strictnethttp.StrictHTTPHandlerFunc, operationID string) strictnethttp.StrictHTTPHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (response any, err error) {
			{
				v := reflect.ValueOf(request)
				if v.Kind() == reflect.Pointer {
					v = v.Elem()
				}
				if v.Kind() != reflect.Struct {
					body := v.FieldByName("Body")
					if body.IsValid() && body.CanInterface() {
						// TODO: use github.com/goccy/go-json for unmarshalling since json/v2 is not available yet.
						if marshaler, err := json.Marshal(body.Interface()); err == nil {
							r.Body = io.NopCloser(bytes.NewReader(marshaler))
						} else {
							return nil, err
						}
					}
				}
			}

			_, validateErrs := highLevelValidator.ValidateHttpRequest(r)
			if len(validateErrs) > 0 {
				resp := utils.ValidationErr{
					Message:        "Validation Error",
					HTTPStatusCode: http.StatusBadRequest,
				}
				for _, validateErr := range validateErrs {
					var reason []string
					for _, schemaErr := range validateErr.SchemaValidationErrors {
						reason = append(reason, schemaErr.Reason)
					}
					resp.Details = append(resp.Details, utils.Details{
						Reason:                validateErr.Reason,
						Fix:                   validateErr.HowToFix,
						SchemaValidationError: reason,
					})
				}
			}
			return next(ctx, w, r, request)
		}
	}
}

func returnMiddleware(f strictnethttp.StrictHTTPHandlerFunc, operationID string) strictnethttp.StrictHTTPHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (response interface{}, err error) {
		return f(ctx, w, r, request)
	}
}
