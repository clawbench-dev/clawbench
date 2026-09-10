package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// clearContainerEnv removes the env-var signals so a marker-file test is not
// short-circuited by an inherited "container" / k8s var (CI images may set one).
func clearContainerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("container", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
}

// stubMarkers points the marker-file probes at test-controlled paths and
// returns a restore func.
func stubMarkers(paths ...string) func() {
	origDocker := dockerMarkers
	dockerMarkers = paths
	return func() { dockerMarkers = origDocker }
}

func TestIsContainer_DockerMarker(t *testing.T) {
	clearContainerEnv(t)
	marker := filepath.Join(t.TempDir(), "dockerenv")
	assert.NoError(t, os.WriteFile(marker, nil, 0o644))
	defer stubMarkers(marker)()

	assert.True(t, IsContainer())
}

func TestIsContainer_PodmanMarker(t *testing.T) {
	clearContainerEnv(t)
	marker := filepath.Join(t.TempDir(), "containerenv")
	assert.NoError(t, os.WriteFile(marker, nil, 0o644))
	defer stubMarkers(marker)()

	assert.True(t, IsContainer())
}

func TestIsContainer_ContainerEnvVar(t *testing.T) {
	clearContainerEnv(t)
	defer stubMarkers(filepath.Join(t.TempDir(), "absent"))()

	t.Setenv("container", "docker")
	assert.True(t, IsContainer())
}

// Kubernetes pods must be detected even when the runtime leaves no marker file:
// the self-restart subprocess path is just as fatal in a pod as in Docker.
func TestIsContainer_KubernetesServiceHost(t *testing.T) {
	clearContainerEnv(t)
	defer stubMarkers(filepath.Join(t.TempDir(), "absent"))()

	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	assert.True(t, IsContainer())
}

func TestIsContainer_NoSignals(t *testing.T) {
	clearContainerEnv(t)
	defer stubMarkers(filepath.Join(t.TempDir(), "absent"))()

	assert.False(t, IsContainer())
}

// A marker that exists but is a directory must still count (the runtime may
// mount it as a directory); the check is presence, not file-ness.
func TestIsContainer_MarkerAsDirectory(t *testing.T) {
	clearContainerEnv(t)
	marker := filepath.Join(t.TempDir(), "containerenv")
	assert.NoError(t, os.Mkdir(marker, 0o755))
	defer stubMarkers(marker)()

	assert.True(t, IsContainer())
}

// ---------- IsDockerLike ----------

func TestIsDockerLike_DockerMarker(t *testing.T) {
	clearContainerEnv(t)
	marker := filepath.Join(t.TempDir(), "dockerenv")
	assert.NoError(t, os.WriteFile(marker, nil, 0o644))
	defer stubMarkers(marker)()

	assert.True(t, IsDockerLike())
}

func TestIsDockerLike_ContainerEnvVar(t *testing.T) {
	clearContainerEnv(t)
	defer stubMarkers(filepath.Join(t.TempDir(), "absent"))()

	t.Setenv("container", "docker")
	assert.True(t, IsDockerLike())
}

// A k8s pod is a container (IsContainer) but NOT "docker-like": the Docker CLI
// advice is not actionable there, so the UI must not show it.
func TestIsDockerLike_KubernetesIsNotDockerLike(t *testing.T) {
	clearContainerEnv(t)
	defer stubMarkers(filepath.Join(t.TempDir(), "absent"))()

	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")

	assert.True(t, IsContainer(), "a pod is a container")
	assert.False(t, IsDockerLike(), "but the Docker CLI advice does not apply")
}

func TestIsDockerLike_NoSignals(t *testing.T) {
	clearContainerEnv(t)
	defer stubMarkers(filepath.Join(t.TempDir(), "absent"))()

	assert.False(t, IsDockerLike())
}
