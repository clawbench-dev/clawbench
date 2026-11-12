package handler

import "clawbench/internal/model"

// configuredTransport returns the server-side tunnel transport ("ssh" or "h2")
// that clients follow.
//
// Read from the live ConfigInstance rather than a startup snapshot: a PATCH can
// switch the transport and the hot-reload path applies it without a restart, so
// a cached copy would keep answering the old way until the next boot.
func configuredTransport() string {
	configMutex.RLock()
	transport := model.ConfigInstance.PortForward.Transport
	configMutex.RUnlock()
	return model.NormalizePortForwardTransport(transport)
}

// h2TransportEnabled reports whether the /api/tunnel/* endpoints must serve.
// With transport "ssh" the server carries forwards over the SSH listener only,
// so those endpoints refuse instead of quietly serving a path the operator
// turned off.
func h2TransportEnabled() bool {
	return configuredTransport() != model.TransportSSH
}
