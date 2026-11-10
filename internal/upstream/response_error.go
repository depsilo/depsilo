package upstream

import (
	"errors"
	"fmt"
	"net/http"
)

// ResponseError reports an HTTP error response to a fetch. Its message keeps
// the established "upstream <name> returned <code> for <origin>" form.
type ResponseError struct {
	Upstream string
	Code     int
	Origin   string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("upstream %s returned %d for %s", e.Upstream, e.Code, e.Origin)
}

// IsNotFound reports whether err carries an upstream 404 response.
func IsNotFound(err error) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Code == http.StatusNotFound
}
