package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
)

type PassthroughDriver struct {
	port   int
	server *http.Server
	ln     net.Listener
}

func NewPassthroughDriver(port int) *PassthroughDriver {
	return &PassthroughDriver{port: port}
}

func (d *PassthroughDriver) Launch(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", d.port))
	if err != nil {
		return err
	}
	d.ln = ln
	d.port = ln.Addr().(*net.TCPAddr).Port

	// Set up reverse proxy to upstream official endpoints
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "https"
			// Route to official LLM provider based on request path
			if strings.HasPrefix(req.URL.Path, "/v1/messages") {
				req.URL.Host = "api.anthropic.com"
				req.Host = "api.anthropic.com"
			} else {
				req.URL.Host = "api.openai.com"
				req.Host = "api.openai.com"
			}
		},
	}

	d.server = &http.Server{
		Handler: proxy,
	}

	go func() {
		if err := d.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Ignore closed server error on shutdown
		}
	}()

	return nil
}

func (d *PassthroughDriver) Shutdown(ctx context.Context) error {
	if d.server != nil {
		return d.server.Shutdown(ctx)
	}
	return nil
}

func (d *PassthroughDriver) ListModels() []ModelInfo {
	// Passthrough has no static model config list
	return nil
}

func (d *PassthroughDriver) Health() HealthStatus {
	return HealthStatus{
		Status: "ok",
		Models: 0,
	}
}

func (d *PassthroughDriver) ProxyURL() string {
	if d.port == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", d.port)
}
