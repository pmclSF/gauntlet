package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/pmclSF/gauntlet/internal/fixture"
)

type proxyRequestError struct {
	StatusCode int
	Code       string
	Message    string
	Cause      error
}

func (e *proxyRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *proxyRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type proxyErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func (p *Proxy) writeConnError(conn net.Conn, err error) {
	statusCode, payload := proxyErrorPayload(err)
	resp := &http.Response{
		StatusCode:    statusCode,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
	}
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Set("Connection", "close")
	_ = resp.Write(conn)
}

func (p *Proxy) writeHTTPError(w http.ResponseWriter, err error) {
	statusCode, payload := proxyErrorPayload(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}

func proxyErrorPayload(err error) (int, []byte) {
	statusCode := http.StatusBadGateway
	code := "proxy_intercept_failed"
	message := "proxy interception failed"

	var perr *proxyRequestError
	if errors.As(err, &perr) {
		statusCode = perr.StatusCode
		code = perr.Code
		message = perr.Message
	} else if _, ok := err.(*fixture.ErrFixtureMiss); ok {
		code = "fixture_miss"
		message = err.Error()
	} else if err != nil {
		message = err.Error()
	}

	if code == "" {
		code = "proxy_intercept_failed"
	}
	if message == "" {
		message = "proxy interception failed"
	}

	payload, marshalErr := json.Marshal(proxyErrorResponse{
		Error: message,
		Code:  code,
	})
	if marshalErr != nil {
		return statusCode, []byte(`{"error":"proxy interception failed","code":"proxy_intercept_failed"}`)
	}
	return statusCode, payload
}
