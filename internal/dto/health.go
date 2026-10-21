package dto

// HealthResponse is returned by the liveness endpoint.
type HealthResponse struct {
	Status string `json:"status"`
}

// ReadyResponse is returned by the readiness endpoint.
type ReadyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}
