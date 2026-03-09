package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Muhammedhashirm009/portix/internal/database"
	"github.com/Muhammedhashirm009/portix/internal/docker"
	"github.com/Muhammedhashirm009/portix/internal/portmanager"
	"github.com/Muhammedhashirm009/portix/internal/tunnel"
	"github.com/gin-gonic/gin"
)

// DockerHandler handles Docker container management API
type DockerHandler struct {
	client    *docker.Client
	tunnelMgr *tunnel.Manager
}

// NewDockerHandler creates a new Docker handler
func NewDockerHandler(tunnelMgr *tunnel.Manager) *DockerHandler {
	return &DockerHandler{
		client:    docker.NewClient(),
		tunnelMgr: tunnelMgr,
	}
}

// --- Async deploy tracking ---

type DeployStatus struct {
	ID           string `json:"id"`
	Status       string `json:"status"` // "cloning", "building", "starting", "tunneling", "done", "failed"
	Step         string `json:"step"`
	Log          string `json:"log"`
	Error        string `json:"error,omitempty"`
	BuildOutput  string `json:"build_output,omitempty"`
	ContainerID  string `json:"container_id,omitempty"`
	Port         int    `json:"port,omitempty"`
	TunnelDomain string `json:"tunnel_domain,omitempty"`
	TunnelStatus string `json:"tunnel_status,omitempty"`
}

var (
	deploys   = map[string]*DeployStatus{}
	deploysMu sync.RWMutex
)

func setDeployStatus(id string, s *DeployStatus) {
	deploysMu.Lock()
	deploys[id] = s
	deploysMu.Unlock()
}

func getDeployStatus(id string) *DeployStatus {
	deploysMu.RLock()
	defer deploysMu.RUnlock()
	return deploys[id]
}

// ListContainers returns all containers
func (h *DockerHandler) ListContainers(c *gin.Context) {
	all := c.DefaultQuery("all", "true") == "true"
	containers, err := h.client.ListContainers(all)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "Docker not available: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": containers})
}

// CreateContainer creates and starts a new container
func (h *DockerHandler) CreateContainer(c *gin.Context) {
	var req struct {
		Name          string            `json:"name"`
		Image         string            `json:"image"`
		Ports         map[string]string `json:"ports"`
		Env           []string          `json:"env"`
		Volumes       map[string]string `json:"volumes"`
		RestartPolicy string            `json:"restart_policy"`
		Command       []string          `json:"command"`
		TunnelDomain  string            `json:"tunnel_domain"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request"})
		return
	}

	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Image is required"})
		return
	}

	if req.Name == "" {
		req.Name = "tp-container"
	}

	log.Printf("[docker] Creating container: %s (image: %s)", req.Name, req.Image)

	createReq := docker.CreateContainerRequest{
		Name:          req.Name,
		Image:         req.Image,
		Ports:         req.Ports,
		Env:           req.Env,
		Volumes:       req.Volumes,
		RestartPolicy: req.RestartPolicy,
		Command:       req.Command,
	}

	containerID, err := h.client.CreateContainer(createReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Auto-tunnel if domain specified
	if req.TunnelDomain != "" && h.tunnelMgr != nil {
		var hostPort int
		for _, hp := range req.Ports {
			p, _ := strconv.Atoi(hp)
			if p > 0 {
				hostPort = p
				break
			}
		}

		if hostPort > 0 {
			pm := portmanager.Get()
			pm.Allocate("docker", 0, req.Name)

			database.DB().Exec(
				"INSERT OR REPLACE INTO docker_apps (container_id, name, domain, mapped_port, image, status) VALUES (?, ?, ?, ?, ?, 'running')",
				containerID, req.Name, req.TunnelDomain, hostPort, req.Image,
			)

			if err := h.tunnelMgr.AddIngressRule(req.TunnelDomain, hostPort, "docker", 0); err != nil {
				log.Printf("[docker] Tunnel ingress failed for %s: %v", req.TunnelDomain, err)
			} else {
				log.Printf("[docker] Tunnel ingress added: %s → localhost:%d", req.TunnelDomain, hostPort)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":   containerID,
			"name": req.Name,
		},
	})
}

// ContainerAction handles start/stop/restart/remove
func (h *DockerHandler) ContainerAction(c *gin.Context) {
	id := c.Param("id")
	action := c.Param("action")

	var err error
	switch action {
	case "start":
		err = h.client.StartContainer(id)
	case "stop":
		err = h.client.StopContainer(id)
	case "restart":
		err = h.client.RestartContainer(id)
	case "remove":
		// Clean up tunnel ingress + Cloudflare DNS if domain exists
		var domain string
		database.DB().QueryRow("SELECT domain FROM docker_apps WHERE container_id = ?", id).Scan(&domain)
		if domain != "" && h.tunnelMgr != nil {
			if err := h.tunnelMgr.RemoveIngressRule(domain); err != nil {
				log.Printf("[docker] Warning: failed to remove tunnel for %s: %v", domain, err)
			} else {
				log.Printf("[docker] Removed tunnel + DNS for container %s (domain: %s)", id, domain)
			}
		}
		database.DB().Exec("DELETE FROM docker_apps WHERE container_id = ?", id)
		err = h.client.RemoveContainer(id, true)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid action: " + action})
		return
	}

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": action + " successful"})
}

// GetContainerLogs returns container logs
func (h *DockerHandler) GetContainerLogs(c *gin.Context) {
	id := c.Param("id")
	tail := 100
	if t := c.Query("tail"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			tail = n
		}
	}

	logs, err := h.client.GetContainerLogs(id, tail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"logs": logs}})
}

// ListImages returns Docker images
func (h *DockerHandler) ListImages(c *gin.Context) {
	images, err := h.client.ListImages()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": images})
}

// PullImage pulls a Docker image
func (h *DockerHandler) PullImage(c *gin.Context) {
	var req struct {
		Image string `json:"image"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Image name is required"})
		return
	}

	log.Printf("[docker] Pulling image: %s", req.Image)
	if err := h.client.PullImage(req.Image); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Image pulled: " + req.Image})
}

// DeployFromRepo starts an async deploy from a Git repository
func (h *DockerHandler) DeployFromRepo(c *gin.Context) {
	var req struct {
		RepoURL      string   `json:"repo_url"`
		Branch       string   `json:"branch"`
		Name         string   `json:"name"`
		Port         int      `json:"port"`
		InternalPort int      `json:"internal_port"`
		Env          []string `json:"env"`
		TunnelDomain string   `json:"tunnel_domain"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request"})
		return
	}

	if req.RepoURL == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Repository URL and name are required"})
		return
	}

	if req.Port == 0 {
		pm := portmanager.Get()
		port, err := pm.Allocate("docker", 0, req.Name)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "Port allocation failed: " + err.Error()})
			return
		}
		req.Port = port
	}

	if req.Branch == "" {
		req.Branch = "main"
	}

	// Generate deploy ID
	deployID := fmt.Sprintf("deploy-%s-%d", req.Name, time.Now().Unix())

	// Set initial status
	status := &DeployStatus{
		ID:           deployID,
		Status:       "cloning",
		Step:         "Cloning repository...",
		Port:         req.Port,
		TunnelDomain: req.TunnelDomain,
	}
	setDeployStatus(deployID, status)

	// Start deploy in background goroutine
	go h.runDeploy(deployID, req.RepoURL, req.Branch, req.Name, req.Port, req.InternalPort, req.Env, req.TunnelDomain)

	// Return immediately with the deploy ID
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"deploy_id": deployID,
			"port":      req.Port,
		},
	})
}

// runDeploy runs the deploy pipeline in the background with step-by-step logging
func (h *DockerHandler) runDeploy(deployID, repoURL, branch, name string, port, internalPort int, env []string, tunnelDomain string) {
	log.Printf("[docker] Deploy %s: starting (repo: %s, branch: %s, port: %d, internal: %d)", deployID, repoURL, branch, port, internalPort)

	status := getDeployStatus(deployID)
	appendLog := func(msg string) {
		status.Log += msg + "\n"
		setDeployStatus(deployID, status)
	}

	// Step 1: Clone
	status.Status = "cloning"
	status.Step = "Cloning repository..."
	appendLog("→ Cloning " + repoURL + " (branch: " + branch + ")")

	cloneOutput, err := h.client.CloneRepo(repoURL, branch, name)
	if err != nil {
		status.Status = "failed"
		status.Step = "Clone failed"
		status.Error = err.Error()
		appendLog("✗ Clone failed: " + err.Error())
		if cloneOutput != "" {
			appendLog(cloneOutput)
		}
		log.Printf("[docker] Deploy %s: clone FAILED — %v", deployID, err)
		return
	}
	appendLog("✓ Repository cloned successfully")

	// Step 2: Build
	status.Status = "building"
	status.Step = "Building Docker image..."
	imageTag := "tp-" + name + ":latest"
	appendLog("→ Building image: " + imageTag)
	appendLog("→ This may take a few minutes...")
	setDeployStatus(deployID, status)

	appDir := "/var/lib/portix/apps/" + name
	buildOutput, err := h.client.BuildImage(appDir, imageTag)
	status.BuildOutput = buildOutput
	if err != nil {
		status.Status = "failed"
		status.Step = "Build failed"
		status.Error = err.Error()
		appendLog("✗ Build failed: " + err.Error())
		if buildOutput != "" {
			appendLog("\n— Build Output —")
			appendLog(buildOutput)
		}
		log.Printf("[docker] Deploy %s: build FAILED — %v", deployID, err)
		return
	}
	appendLog("✓ Image built successfully")
	if buildOutput != "" {
		appendLog("\n— Build Output —")
		appendLog(buildOutput)
	}

	// Step 3: Create and start container
	status.Status = "starting"
	status.Step = "Starting container..."
	if internalPort == 0 {
		internalPort = port
	}
	appendLog(fmt.Sprintf("→ Starting container (host:%d → container:%d)", port, internalPort))
	setDeployStatus(deployID, status)

	hostPortStr := fmt.Sprintf("%d", port)
	containerPortStr := fmt.Sprintf("%d", internalPort)
	createReq := docker.CreateContainerRequest{
		Name:  name,
		Image: imageTag,
		Ports: map[string]string{
			containerPortStr + "/tcp": hostPortStr,
		},
		Env:           env,
		RestartPolicy: "unless-stopped",
	}

	containerID, err := h.client.CreateContainer(createReq)
	if err != nil {
		status.Status = "failed"
		status.Step = "Container start failed"
		status.Error = err.Error()
		appendLog("✗ Container failed: " + err.Error())
		log.Printf("[docker] Deploy %s: container FAILED — %v", deployID, err)
		return
	}
	status.ContainerID = containerID
	appendLog("✓ Container started: " + containerID)

	// Step 4: Auto-tunnel
	if tunnelDomain != "" && h.tunnelMgr != nil {
		status.Status = "tunneling"
		status.Step = "Setting up tunnel for " + tunnelDomain + "..."
		appendLog("→ Setting up tunnel: " + tunnelDomain + " → localhost:" + hostPortStr)
		setDeployStatus(deployID, status)

		database.DB().Exec(
			"INSERT OR REPLACE INTO docker_apps (container_id, name, domain, mapped_port, image, status) VALUES (?, ?, ?, ?, ?, 'running')",
			containerID, name, tunnelDomain, port, imageTag,
		)

		if err := h.tunnelMgr.AddIngressRule(tunnelDomain, port, "docker", 0); err != nil {
			status.TunnelStatus = "Tunnel failed: " + err.Error()
			appendLog("✗ Tunnel setup failed: " + err.Error())
			log.Printf("[docker] Deploy %s: tunnel failed — %v", deployID, err)
		} else {
			status.TunnelStatus = "Tunneled to https://" + tunnelDomain
			appendLog("✓ Tunnel active: https://" + tunnelDomain)
			log.Printf("[docker] Deploy %s: tunnel added %s → localhost:%d", deployID, tunnelDomain, port)
		}
	}

	// Done
	status.Status = "done"
	status.Step = "Deployment complete!"
	appendLog("\n✓ Deployment complete!")
	setDeployStatus(deployID, status)
	log.Printf("[docker] Deploy %s: SUCCESS (container: %s, port: %d)", deployID, containerID, port)
}

// GetDeployStatus returns the current status of an async deploy
func (h *DockerHandler) GetDeployStatus(c *gin.Context) {
	id := c.Param("id")
	status := getDeployStatus(id)
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Deploy not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}
