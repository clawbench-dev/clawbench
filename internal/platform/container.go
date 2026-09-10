package platform

import "os"

// dockerMarkers are filesystem markers created by Docker / Podman inside the
// container's own filesystem.
//
//   - /.dockerenv          Docker (and most OCI runtimes built on runc)
//   - /run/.containerenv   Podman
var dockerMarkers = []string{
	"/.dockerenv",
	"/run/.containerenv",
}

// containerEnvVars are environment variables that identify a container of any
// kind.
//
//   - container: set by systemd-nspawn / some runtimes when the process runs
//     in a container (the de-facto "am I containerized" hint).
//   - KUBERNETES_SERVICE_HOST: injected by Kubernetes into every pod. A pod is
//     a container even when the runtime leaves no marker file, so this is the
//     reliable signal for k8s.
var containerEnvVars = []string{
	"container",
	"KUBERNETES_SERVICE_HOST",
}

// dockerEnvVars are the env signals that specifically indicate Docker/Podman
// (not just any container). Kubernetes is excluded on purpose — see IsDockerLike.
var dockerEnvVars = []string{
	"container",
}

func anyEnvSet(keys []string) bool {
	for _, key := range keys {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

func anyPathExists(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// IsContainer reports whether the process is running inside a container of any
// kind: Docker, Podman, Kubernetes, ...
//
// Use this — not IsDockerLike — to decide whether self-restart machinery can
// work. In a container the runtime tears the namespace down when PID 1 exits,
// killing every helper process, so a sentinel or `upgrade-replace` subprocess
// can never complete its work; only the runtime's restart policy brings the
// service back. That is true in a k8s pod just as much as in Docker, so the
// check must be the broad one.
func IsContainer() bool {
	return anyPathExists(dockerMarkers) || anyEnvSet(containerEnvVars)
}

// IsDockerLike reports whether the process is running under Docker or Podman
// specifically — i.e. where Docker-CLI advice (`docker pull`, `docker compose`)
// is actionable.
//
// Deliberately narrower than IsContainer: a Kubernetes pod is a container but
// the Docker CLI advice does not apply, so the UI must not show it there.
// Use IsContainer for supervision/self-restart decisions.
func IsDockerLike() bool {
	return anyPathExists(dockerMarkers) || anyEnvSet(dockerEnvVars)
}
