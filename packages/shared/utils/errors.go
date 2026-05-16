package utils

import (
	"github.com/go-chi/render"
	"net/http"
)

type ValidationErr struct {
	Message        string               `json:"message"`
	Details        []Details            `json:"schema,omitempty"`
	Parameters     map[string][]Details `json:"fields,omitempty"`
	HTTPStatusCode int                  `json:"-"` // http response status code
}

type Details struct {
	Reason                string   `json:"reason,omitempty"`
	Fix                   string   `json:"fix,omitempty"`
	SchemaValidationError []string `json:"schema_validation_error,omitempty"`
}

func (e ValidationErr) Error() string {
	return e.Message
}

func (e ValidationErr) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}
