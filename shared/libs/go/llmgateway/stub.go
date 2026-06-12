package llmgateway

import "context"

// StubGateway is a no-op implementation of LLMGatewayBackend for testing.
// It records whether Launch/Shutdown were called via public flags.
type StubGateway struct {
	Launched bool
	ShutDown bool
}

// NewStubGateway creates a StubGateway.
func NewStubGateway() *StubGateway {
	return &StubGateway{}
}

// Launch sets Launched to true. Always succeeds.
func (s *StubGateway) Launch(_ context.Context) error {
	s.Launched = true
	return nil
}

// Shutdown sets ShutDown to true. Always succeeds.
func (s *StubGateway) Shutdown(_ context.Context) error {
	s.ShutDown = true
	return nil
}

// ListModels returns nil (no configured models).
func (s *StubGateway) ListModels() []ModelInfo {
	return nil
}

// DefaultModel returns nil (no default model in stub).
func (s *StubGateway) DefaultModel() *ModelInfo {
	return nil
}

// Health returns a stub health status.
func (s *StubGateway) Health() HealthStatus {
	return HealthStatus{Status: "stub"}
}

// ProxyURL returns an empty string (no proxy running).
func (s *StubGateway) ProxyURL() string {
	return ""
}
