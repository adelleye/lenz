package httpmiddleware

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/spf13/viper"
	"io"
	"lenz-core/packages/shared/utils"
	"net/http"
	"strings"
)

func OAPIResponseValidator(spec *openapi3.T) func(next http.Handler) http.Handler {
	if viper.GetString(utils.Env) != utils.DevelopmentEnv {
		return func(next http.Handler) http.Handler { return next }
	}

	b, err := spec.MarshalJSON()
	if err != nil {
		return func(next http.Handler) http.Handler { return next }
	}

	doc, err := libopenapi.NewDocument(b)
	if err != nil {
		return func(next http.Handler) http.Handler { return next }
	}

	highLevelValidator, validatorErrs := validator.NewValidator(doc)
	if len(validatorErrs) > 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// populate the request from the application
			wrapper, ok := w.(middleware.WrapResponseWriter)
			if !ok {
				wrapper = middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			}
			buf := bytes.NewBuffer(nil)
			wrapper.Tee(buf)

			next.ServeHTTP(wrapper, r)

			response := &http.Response{
				StatusCode: wrapper.Status(),
				Header:     wrapper.Header(),
				Body:       io.NopCloser(buf),
			}

			requestValid, validationErrs := highLevelValidator.ValidateHttpResponse(r, response)
			if !requestValid {
				var errDetails []string
				for _, err := range validationErrs {
					errDetails = append(errDetails, err.Message)
					for _, schemaErr := range err.SchemaValidationErrors {
						errDetails = append(errDetails, fmt.Sprintf("schema validation error: %s, location: %s", schemaErr.Reason, schemaErr.Location))
					}
				}
				validationErr := errors.New(strings.Join(errDetails, "\n"))
				fmt.Printf("validation error: %s\n", validationErr.Error())
				// log out the response to
			}
		})
	}
}
