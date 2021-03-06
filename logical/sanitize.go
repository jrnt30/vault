package logical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// This logic was pulled from the http package so that it can be used for
// encoding wrapped responses as well. It simply translates the logical request
// to an http response, with the values we want and omitting the values we
// don't.
func SanitizeResponse(input *Response) *HTTPResponse {
	logicalResp := &HTTPResponse{
		Data:     input.Data,
		Warnings: input.Warnings(),
	}

	if input.Secret != nil {
		logicalResp.LeaseID = input.Secret.LeaseID
		logicalResp.Renewable = input.Secret.Renewable
		logicalResp.LeaseDuration = int(input.Secret.TTL.Seconds())
	}

	// If we have authentication information, then
	// set up the result structure.
	if input.Auth != nil {
		logicalResp.Auth = &HTTPAuth{
			ClientToken:   input.Auth.ClientToken,
			Accessor:      input.Auth.Accessor,
			Policies:      input.Auth.Policies,
			Metadata:      input.Auth.Metadata,
			LeaseDuration: int(input.Auth.TTL.Seconds()),
			Renewable:     input.Auth.Renewable,
		}
	}

	return logicalResp
}

type HTTPResponse struct {
	RequestID     string                 `json:"request_id"`
	LeaseID       string                 `json:"lease_id"`
	Renewable     bool                   `json:"renewable"`
	LeaseDuration int                    `json:"lease_duration"`
	Data          map[string]interface{} `json:"data"`
	WrapInfo      *HTTPWrapInfo          `json:"wrap_info"`
	Warnings      []string               `json:"warnings"`
	Auth          *HTTPAuth              `json:"auth"`
}

type HTTPAuth struct {
	ClientToken   string            `json:"client_token"`
	Accessor      string            `json:"accessor"`
	Policies      []string          `json:"policies"`
	Metadata      map[string]string `json:"metadata"`
	LeaseDuration int               `json:"lease_duration"`
	Renewable     bool              `json:"renewable"`
}

type HTTPWrapInfo struct {
	Token           string    `json:"token"`
	TTL             int       `json:"ttl"`
	CreationTime    time.Time `json:"creation_time"`
	WrappedAccessor string    `json:"wrapped_accessor,omitempty"`
}

type HTTPSysInjector struct {
	Response *HTTPResponse
}

func (h HTTPSysInjector) MarshalJSON() ([]byte, error) {
	j, err := json.Marshal(h.Response)
	if err != nil {
		return nil, err
	}

	// Fast path no data or empty data
	if h.Response.Data == nil || len(h.Response.Data) == 0 {
		return j, nil
	}

	// Marshaling a response will always be a JSON object, meaning it will
	// always start with '{', so we hijack this to prepend necessary values

	// Make a guess at the capacity, and write the object opener
	buf := bytes.NewBuffer(make([]byte, 0, len(j)*2))
	buf.WriteRune('{')

	for k, v := range h.Response.Data {
		// Marshal each key/value individually
		mk, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		mv, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		// Write into the final buffer. We'll never have a valid response
		// without any fields so we can unconditionally add a comma after each.
		buf.WriteString(fmt.Sprintf("%s: %s, ", mk, mv))
	}

	// Add the rest, without the first '{'
	buf.Write(j[1:])

	return buf.Bytes(), nil
}
