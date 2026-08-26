package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

// parseID reads a resource id, naming the kind of resource in the failure
// so that a mistyped id says which attribute it came from. The SDK's id
// parameters are aliases of uuid.UUID.
func parseID(id, kind string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("%q is not a %s id: %w", id, kind, err)
	}
	return parsed, nil
}

// decodeBody reads an answer into out and releases the response, which the
// SDK's transport needs in order to end the attempt's deadline.
func decodeBody(resp *http.Response, out any) error {
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// drain releases an answer whose body carries nothing this client reads.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
