package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Container represents a Docker container
type Container struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Status  string            `json:"status"`
	State   string            `json:"state"`
	Ports   []PortMapping     `json:"ports"`
	Created int64             `json:"created"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// PortMapping represents a port mapping
type PortMapping struct {
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port"`
	Type        string `json:"type"`
}

// Image represents a Docker image
type Image struct {
	ID      string   `json:"id"`
	Tags    []string `json:"tags"`
	Size    int64    `json:"size"`
	Created int64    `json:"created"`
}

// CreateContainerRequest holds container creation params
type CreateContainerRequest struct {
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	Ports        map[string]string `json:"ports"`        // "8080/tcp" -> "8080"
	Env          []string          `json:"env"`           // ["KEY=VALUE"]
	Volumes      map[string]string `json:"volumes"`       // "/host/path" -> "/container/path"
	RestartPolicy string          `json:"restart_policy"` // "always", "unless-stopped", "on-failure", ""
	Command      []string          `json:"command"`
}

// Client communicates with Docker Engine API via Unix socket
type Client struct {
	httpc *http.Client
}

// NewClient creates a Docker API client
func NewClient() *Client {
	return &Client{
		httpc: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.DialTimeout("unix", "/var/run/docker.sock", 5*time.Second)
				},
			},
			Timeout: 120 * time.Second,
		},
	}
}

// Ping checks if Docker daemon is accessible
func (c *Client) Ping() error {
	resp, err := c.do("GET", "/_ping", nil)
	if err != nil {
		return fmt.Errorf("docker not reachable: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// ListContainers returns all containers
func (c *Client) ListContainers(all bool) ([]Container, error) {
	url := "/containers/json"
	if all {
		url += "?all=true"
	}

	resp, err := c.do("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw []struct {
		ID      string   `json:"Id"`
		Names   []string `json:"Names"`
		Image   string   `json:"Image"`
		Status  string   `json:"Status"`
		State   string   `json:"State"`
		Created int64    `json:"Created"`
		Labels  map[string]string `json:"Labels"`
		Ports   []struct {
			PrivatePort int    `json:"PrivatePort"`
			PublicPort  int    `json:"PublicPort"`
			Type        string `json:"Type"`
		} `json:"Ports"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode containers: %w", err)
	}

	containers := make([]Container, len(raw))
	for i, r := range raw {
		name := ""
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}
		ports := make([]PortMapping, len(r.Ports))
		for j, p := range r.Ports {
			ports[j] = PortMapping{
				PrivatePort: p.PrivatePort,
				PublicPort:  p.PublicPort,
				Type:        p.Type,
			}
		}
		containers[i] = Container{
			ID:      r.ID[:12],
			Name:    name,
			Image:   r.Image,
			Status:  r.Status,
			State:   r.State,
			Ports:   ports,
			Created: r.Created,
			Labels:  r.Labels,
		}
	}
	return containers, nil
}

// CreateContainer creates and starts a container
func (c *Client) CreateContainer(req CreateContainerRequest) (string, error) {
	// Build Docker API request body
	body := map[string]interface{}{
		"Image": req.Image,
		"Env":   req.Env,
	}

	if len(req.Command) > 0 {
		body["Cmd"] = req.Command
	}

	// Port bindings
	exposedPorts := map[string]interface{}{}
	portBindings := map[string]interface{}{}
	for containerPort, hostPort := range req.Ports {
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []map[string]string{
			{"HostPort": hostPort},
		}
	}
	body["ExposedPorts"] = exposedPorts

	// Host config
	hostConfig := map[string]interface{}{
		"PortBindings": portBindings,
	}

	// Volumes / Binds
	if len(req.Volumes) > 0 {
		binds := []string{}
		for hostPath, containerPath := range req.Volumes {
			binds = append(binds, hostPath+":"+containerPath)
		}
		hostConfig["Binds"] = binds
	}

	// Restart policy
	if req.RestartPolicy != "" {
		hostConfig["RestartPolicy"] = map[string]interface{}{
			"Name": req.RestartPolicy,
		}
	}

	body["HostConfig"] = hostConfig

	// Create
	url := "/containers/create"
	if req.Name != "" {
		url += "?name=" + req.Name
	}

	resp, err := c.doJSON("POST", url, body)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create failed (%d): %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		ID string `json:"Id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	containerID := result.ID

	// Start the container
	startResp, err := c.do("POST", "/containers/"+containerID+"/start", nil)
	if err != nil {
		return containerID, fmt.Errorf("start container: %w", err)
	}
	defer startResp.Body.Close()

	log.Printf("[docker] Container created and started: %s (%s)", req.Name, containerID[:12])
	return containerID[:12], nil
}

// StartContainer starts a stopped container
func (c *Client) StartContainer(id string) error {
	resp, err := c.do("POST", "/containers/"+id+"/start", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 304 {
		return nil // already started
	}
	if resp.StatusCode != 204 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("start failed (%d): %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// StopContainer stops a running container
func (c *Client) StopContainer(id string) error {
	resp, err := c.do("POST", "/containers/"+id+"/stop?t=10", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 304 {
		return nil // already stopped
	}
	if resp.StatusCode != 204 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stop failed (%d): %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// RestartContainer restarts a container
func (c *Client) RestartContainer(id string) error {
	resp, err := c.do("POST", "/containers/"+id+"/restart?t=10", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("restart failed (%d): %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// RemoveContainer removes a container (force stops first)
func (c *Client) RemoveContainer(id string, force bool) error {
	url := "/containers/" + id + "?v=true"
	if force {
		url += "&force=true"
	}
	resp, err := c.do("DELETE", url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remove failed (%d): %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// GetContainerLogs returns the last N lines of logs
func (c *Client) GetContainerLogs(id string, tail int) (string, error) {
	url := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d&timestamps=true", id, tail)
	resp, err := c.do("GET", url, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Docker log output has 8-byte header per line — strip it
	lines := []string{}
	data := raw
	for len(data) > 0 {
		if len(data) < 8 {
			break
		}
		// First 4 bytes: stream type + padding, next 4 bytes: size (big-endian)
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		line := strings.TrimRight(string(data[:size]), "\n\r")
		if line != "" {
			lines = append(lines, line)
		}
		data = data[size:]
	}

	return strings.Join(lines, "\n"), nil
}

// ListImages returns local Docker images
func (c *Client) ListImages() ([]Image, error) {
	resp, err := c.do("GET", "/images/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw []struct {
		ID          string   `json:"Id"`
		RepoTags    []string `json:"RepoTags"`
		Size        int64    `json:"Size"`
		Created     int64    `json:"Created"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode images: %w", err)
	}

	images := make([]Image, len(raw))
	for i, r := range raw {
		id := r.ID
		if strings.HasPrefix(id, "sha256:") {
			id = id[7:]
		}
		if len(id) > 12 {
			id = id[:12]
		}
		images[i] = Image{
			ID:      id,
			Tags:    r.RepoTags,
			Size:    r.Size,
			Created: r.Created,
		}
	}
	return images, nil
}

// PullImage pulls an image (blocking)
func (c *Client) PullImage(image string) error {
	url := "/images/create?fromImage=" + image
	resp, err := c.do("POST", url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Consume the stream to wait for completion
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Errorf("pull stream error: %w", err)
	}
	log.Printf("[docker] Image pulled: %s", image)
	return nil
}

// CloneRepo clones a git repository into /var/lib/tunnelpanel/apps/<name>
func (c *Client) CloneRepo(repoURL, branch, name string) (string, error) {
	appsDir := "/var/lib/tunnelpanel/apps"
	os.MkdirAll(appsDir, 0755)

	appDir := filepath.Join(appsDir, name)
	os.RemoveAll(appDir)

	cloneArgs := []string{"clone", "--depth", "1"}
	if branch != "" {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, repoURL, appDir)

	cmd := exec.Command("git", cloneArgs...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("git clone failed: %s — %w", output, err)
	}

	// Check for Dockerfile
	dockerfilePath := filepath.Join(appDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return output, fmt.Errorf("no Dockerfile found in repository root")
	}

	log.Printf("[docker] Clone complete: %s", appDir)
	return output, nil
}

// DeployFromRepo clones a Git repo, builds a Docker image, and runs a container
func (c *Client) DeployFromRepo(repoURL, branch, name string, port, internalPort int, envVars []string) (string, string, error) {
	appsDir := "/var/lib/tunnelpanel/apps"
	os.MkdirAll(appsDir, 0755)

	appDir := filepath.Join(appsDir, name)

	// Clean up any previous clone
	os.RemoveAll(appDir)

	// Step 1: Clone the repo
	log.Printf("[docker] Cloning %s (branch: %s) into %s", repoURL, branch, appDir)
	cloneArgs := []string{"clone", "--depth", "1"}
	if branch != "" {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, repoURL, appDir)

	cmd := exec.Command("git", cloneArgs...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git clone failed: %s — %w", string(out), err)
	}
	log.Printf("[docker] Clone complete: %s", appDir)

	// Step 2: Check for Dockerfile
	dockerfilePath := filepath.Join(appDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("no Dockerfile found in repository root")
	}

	// Step 3: Build Docker image using docker CLI
	imageTag := "tp-" + name + ":latest"
	log.Printf("[docker] Building image: %s", imageTag)
	buildOutput, err := c.BuildImage(appDir, imageTag)
	if err != nil {
		return "", buildOutput, fmt.Errorf("docker build failed: %w", err)
	}
	log.Printf("[docker] Build complete: %s", imageTag)

	// Step 4: Create and start container
	// Use internalPort as the container port, port as the host port
	if internalPort == 0 {
		internalPort = port // If no internal port specified, use same as host port
	}
	hostPortStr := fmt.Sprintf("%d", port)
	containerPortStr := fmt.Sprintf("%d", internalPort)

	createReq := CreateContainerRequest{
		Name:  name,
		Image: imageTag,
		Ports: map[string]string{
			containerPortStr + "/tcp": hostPortStr,
		},
		Env:           envVars,
		RestartPolicy: "unless-stopped",
	}

	containerID, err := c.CreateContainer(createReq)
	if err != nil {
		return "", buildOutput, fmt.Errorf("container creation failed: %w", err)
	}

	log.Printf("[docker] Deployed: %s → container %s (host:%d → container:%d)", name, containerID, port, internalPort)
	return containerID, buildOutput, nil
}

// BuildImage builds a Docker image from a directory using docker CLI
func (c *Client) BuildImage(contextDir, tag string) (string, error) {
	cmd := exec.Command("docker", "build", "-t", tag, ".")
	cmd.Dir = contextDir
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("build error: %s — %w", output, err)
	}
	return output, nil
}

// --- Internal helpers ---

func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, "http://localhost/v1.43"+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpc.Do(req)
}

func (c *Client) doJSON(method, path string, data interface{}) (*http.Response, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return c.do(method, path, strings.NewReader(string(b)))
}
