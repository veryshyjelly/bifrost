package providers

import (
	"io"
	"net/http"
)

// CloudTransport negotiates HTTP/2 on a fresh connection for each request.
// A non-replayable body prevents an opaque mutation replay after GOAWAY.
func CloudTransport() http.RoundTripper {
	return &cloudTransport{base: http.DefaultTransport.(*http.Transport)}
}

type cloudTransport struct{ base *http.Transport }

func (c *cloudTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t := c.base.Clone()
	t.DisableKeepAlives = true
	t.Protocols = new(http.Protocols)
	t.Protocols.SetHTTP1(true)
	t.Protocols.SetHTTP2(true)
	copy := request.Clone(request.Context())
	copy.GetBody = nil
	body := copy.Body
	if body == nil {
		body = http.NoBody
	}
	copy.Body = &cloudRequestBody{body}
	response, err := t.RoundTrip(copy)
	if err != nil {
		t.CloseIdleConnections()
		return nil, err
	}
	response.Body = &cloudResponseBody{ReadCloser: response.Body, transport: t}
	return response, nil
}

type cloudRequestBody struct{ io.ReadCloser }
type cloudResponseBody struct {
	io.ReadCloser
	transport *http.Transport
}

func (b *cloudResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.transport.CloseIdleConnections()
	return err
}
