package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Config struct {
	SSHHost      string `json:"ssh_host"`
	SSHPort      string `json:"ssh_port"`
	SSHUser      string `json:"ssh_user"`
	SSHKeyPath   string `json:"ssh_key_path"`
	SSHPassword  string `json:"ssh_password"`
	AuthMethod   string `json:"auth_method"` // "password" or "key"
	WorkingDir   string `json:"working_dir"`
	GitHubToken  string `json:"github_token"`
	IsConfigured bool   `json:"is_configured"`
}

type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type GitOperation struct {
	Type      string    `json:"type"`
	RepoURL   string    `json:"repo_url"`
	Message   string    `json:"message"`
	Branch    string    `json:"branch"`
	Timestamp time.Time `json:"timestamp"`
}

type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

type SSHManager struct {
	config *Config
	client *ssh.Client
}

func NewSSHManager(config *Config) *SSHManager {
	return &SSHManager{config: config}
}

func (s *SSHManager) Connect() error {
	var authMethods []ssh.AuthMethod

	if s.config.AuthMethod == "password" {
		// Password authentication
		authMethods = append(authMethods, ssh.Password(s.config.SSHPassword))
	} else {
		// SSH key authentication
		keyBytes, err := os.ReadFile(s.config.SSHKeyPath)
		if err != nil {
			return fmt.Errorf("SSH key read failed: %v", err)
		}

		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return fmt.Errorf("SSH key parse failed: %v", err)
		}

		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	config := &ssh.ClientConfig{
		User:            s.config.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	var err error
	s.client, err = ssh.Dial("tcp", s.config.SSHHost+":"+s.config.SSHPort, config)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %v", err)
	}

	return nil
}

func (s *SSHManager) ExecuteCommand(command string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("SSH connection not established")
	}

	// Log command
	log.Printf("📋 SSH Command: %s", command)

	session, err := s.client.NewSession()
	if err != nil {
		log.Printf("❌ Session creation failed: %v", err)
		return "", err
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	outputStr := string(output)

	if err != nil {
		log.Printf("❌ Command failed: %s -> Error: %v, Output: %s", command, err, outputStr)
	} else {
		log.Printf("✅ Command success: %s -> Output: %s", command, outputStr)
	}

	return outputStr, err
}

func (s *SSHManager) ListProjects() ([]Project, error) {
	// Find Git repositories in working directory
	command := fmt.Sprintf("find %s -maxdepth 2 -name '.git' -type d", s.config.WorkingDir)
	log.Printf("🔍 Searching for Git repositories: %s", command)

	output, err := s.ExecuteCommand(command)
	if err != nil {
		log.Printf("❌ Git repository search failed: %v", err)
		return nil, err
	}

	var projects []Project
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Get parent directory of .git folder - Linux path format
		projectPath := strings.Replace(filepath.Dir(line), "\\", "/", -1)
		projectName := filepath.Base(projectPath)

		// Skip empty names and those starting with .
		if projectName == "" || strings.HasPrefix(projectName, ".") {
			continue
		}

		project := Project{
			Name: projectName,
			Path: projectPath,
		}
		projects = append(projects, project)
		log.Printf("📁 Project found: %s -> %s", projectName, projectPath)
	}

	log.Printf("✅ Total %d projects found", len(projects))
	return projects, nil
}

func (s *SSHManager) ListFiles(path string) ([]FileInfo, error) {
	if path == "" {
		path = s.config.WorkingDir
	}

	command := fmt.Sprintf("find %s -maxdepth 1 -type f -exec ls -la {} \\; && find %s -maxdepth 1 -type d -exec ls -ld {} \\;", path, path)
	output, err := s.ExecuteCommand(command)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 9 {
			name := parts[8]
			if name == "." || name == ".." {
				continue
			}

			file := FileInfo{
				Name:    name,
				Path:    filepath.Join(path, name),
				IsDir:   strings.HasPrefix(line, "d"),
				ModTime: strings.Join(parts[5:8], " "),
			}
			files = append(files, file)
		}
	}

	return files, nil
}

func (s *SSHManager) GitClone(repoURL, branch string) (string, error) {
	log.Printf("📥 Clone starting: %s (branch: %s)", repoURL, branch)

	// Add GitHub token to URL if available
	if s.config.GitHubToken != "" {
		repoURL = s.addTokenToURL(repoURL)
		log.Printf("🔐 GitHub token added")
	}

	var command string
	if branch != "" {
		command = fmt.Sprintf("cd %s && git clone -b %s %s", s.config.WorkingDir, branch, repoURL)
	} else {
		command = fmt.Sprintf("cd %s && git clone %s", s.config.WorkingDir, repoURL)
	}

	result, err := s.ExecuteCommand(command)
	if err != nil {
		log.Printf("❌ Clone failed: %v", err)
	} else {
		log.Printf("✅ Clone successful")
	}
	return result, err
}

func (s *SSHManager) GitPull(repoPath string) (string, error) {
	// Convert to Linux path format
	repoPath = strings.Replace(repoPath, "\\", "/", -1)
	log.Printf("⬇️ Pull starting: %s", repoPath)

	// Update remote URL with GitHub token if available
	if s.config.GitHubToken != "" {
		getRemoteCmd := fmt.Sprintf("cd %s && git remote get-url origin", repoPath)
		remoteURL, err := s.ExecuteCommand(getRemoteCmd)
		if err == nil && strings.TrimSpace(remoteURL) != "" {
			tokenURL := s.addTokenToURL(strings.TrimSpace(remoteURL))
			setURLCmd := fmt.Sprintf("cd %s && git remote set-url origin %s", repoPath, tokenURL)
			s.ExecuteCommand(setURLCmd)
			log.Printf("🔐 Remote URL updated with token")
		}
	}

	command := fmt.Sprintf("cd %s && git pull", repoPath)
	result, err := s.ExecuteCommand(command)
	if err != nil {
		log.Printf("❌ Pull failed: %v", err)
	} else {
		log.Printf("✅ Pull successful")
	}
	return result, err
}

func (s *SSHManager) GitPush(repoPath, message string) (string, error) {
	// Convert to Linux path format
	repoPath = strings.Replace(repoPath, "\\", "/", -1)
	log.Printf("⬆️ Push starting: %s (message: %s)", repoPath, message)

	// Update remote URL with GitHub token if available
	if s.config.GitHubToken != "" {
		getRemoteCmd := fmt.Sprintf("cd %s && git remote get-url origin", repoPath)
		remoteURL, err := s.ExecuteCommand(getRemoteCmd)
		if err == nil && strings.TrimSpace(remoteURL) != "" {
			tokenURL := s.addTokenToURL(strings.TrimSpace(remoteURL))
			setURLCmd := fmt.Sprintf("cd %s && git remote set-url origin %s", repoPath, tokenURL)
			s.ExecuteCommand(setURLCmd)
			log.Printf("🔐 Remote URL updated with token")
		}
	}

	commands := []string{
		fmt.Sprintf("cd %s && git add .", repoPath),
		fmt.Sprintf("cd %s && git commit -m \"%s\"", repoPath, message),
		fmt.Sprintf("cd %s && git push", repoPath),
	}

	var results []string
	for i, cmd := range commands {
		log.Printf("📋 Push step %d: %s", i+1, cmd)
		result, err := s.ExecuteCommand(cmd)
		if err != nil {
			log.Printf("❌ Push step %d failed: %v", i+1, err)
			return fmt.Sprintf("%s\nError: %v", result, err), err
		}
		results = append(results, result)
	}

	log.Printf("✅ Push successful")
	return strings.Join(results, "\n"), nil
}

func (s *SSHManager) GitStatus(repoPath string) (string, error) {
	// Convert to Linux path format
	repoPath = strings.Replace(repoPath, "\\", "/", -1)
	log.Printf("📊 Status checking: %s", repoPath)

	command := fmt.Sprintf("cd %s && git status", repoPath)
	result, err := s.ExecuteCommand(command)
	if err != nil {
		log.Printf("❌ Status failed: %v", err)
	} else {
		log.Printf("✅ Status successful")
	}
	return result, err
}

func (s *SSHManager) RemoveProject(repoPath string) (string, error) {
	// Convert to Linux path format
	repoPath = strings.Replace(repoPath, "\\", "/", -1)
	log.Printf("🗑️ Project removing: %s", repoPath)

	// First check if directory exists
	checkCmd := fmt.Sprintf("test -d %s && echo 'exists' || echo 'not exists'", repoPath)
	checkResult, _ := s.ExecuteCommand(checkCmd)
	log.Printf("📁 Directory existence: %s", strings.TrimSpace(checkResult))

	// Remove directory
	command := fmt.Sprintf("rm -rf %s", repoPath)
	result, err := s.ExecuteCommand(command)

	// Confirm deletion
	confirmCmd := fmt.Sprintf("test -d %s && echo 'still exists' || echo 'deleted'", repoPath)
	confirmResult, _ := s.ExecuteCommand(confirmCmd)
	log.Printf("🔍 Removal result: %s", strings.TrimSpace(confirmResult))

	if err != nil {
		log.Printf("❌ Remove failed: %v", err)
	} else {
		log.Printf("✅ Remove successful")
	}

	return fmt.Sprintf("Command: %s\nResult: %s\nConfirm: %s", command, result, confirmResult), err
}

func (s *SSHManager) addTokenToURL(repoURL string) string {
	// Replace GitHub HTTPS URL with token
	if strings.Contains(repoURL, "github.com") && strings.HasPrefix(repoURL, "https://") {
		// https://github.com/user/repo.git -> https://token@github.com/user/repo.git
		repoURL = strings.Replace(repoURL, "https://github.com", fmt.Sprintf("https://%s@github.com", s.config.GitHubToken), 1)
	}
	return repoURL
}

func (s *SSHManager) Disconnect() {
	if s.client != nil {
		s.client.Close()
	}
}

// HTTP Handlers
var sshManager *SSHManager
var config *Config

func main() {
	// Load config
	config = loadConfig()
	sshManager = NewSSHManager(config)

	// SSH connection (if configured)
	if config.IsConfigured {
		if err := sshManager.Connect(); err != nil {
			log.Printf("SSH connection error: %v", err)
		}
	}

	// HTTP routes
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/setup", setupHandler)
	http.HandleFunc("/save-config", saveConfigHandler)
	http.HandleFunc("/test-connection", testConnectionHandler)
	http.HandleFunc("/projects", projectsHandler)
	http.HandleFunc("/git/clone", gitCloneHandler)
	http.HandleFunc("/git/pull", gitPullHandler)
	http.HandleFunc("/git/push", gitPushHandler)
	http.HandleFunc("/git/status", gitStatusHandler)
	http.HandleFunc("/git/remove", gitRemoveHandler)
	http.HandleFunc("/config", configHandler)

	// Static files
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static/"))))

	log.Println("Server started: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadConfig() *Config {
	data, err := os.ReadFile("config.json")
	if err != nil {
		// Default config
		return &Config{
			SSHHost:      "",
			SSHPort:      "22",
			SSHUser:      "root",
			SSHKeyPath:   "",
			SSHPassword:  "",
			AuthMethod:   "password",
			WorkingDir:   "/root/projects",
			GitHubToken:  "",
			IsConfigured: false,
		}
	}

	var cfg Config
	json.Unmarshal(data, &cfg)
	return &cfg
}

func saveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("config.json", data, 0644)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// Redirect to setup if not configured
	if !config.IsConfigured {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	tmpl := `<!DOCTYPE html>
<html>
<head>
    <title>SSH GitHub Manager</title>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .header { text-align: center; margin-bottom: 30px; color: #333; }
        .config-info { background: #e8f4f8; padding: 15px; border-radius: 5px; margin-bottom: 20px; }
        .section { margin: 20px 0; padding: 20px; border: 1px solid #ddd; border-radius: 5px; }
        .git-actions { display: flex; gap: 10px; flex-wrap: wrap; }
        .btn { padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; }
        .btn:hover { background: #0056b3; }
        .btn-success { background: #28a745; }
        .btn-success:hover { background: #1e7e34; }
        .btn-warning { background: #ffc107; color: #212529; }
        .btn-warning:hover { background: #e0a800; }
        .btn-danger { background: #dc3545; }
        .btn-danger:hover { background: #c82333; }
        .btn-secondary { background: #6c757d; }
        .btn-secondary:hover { background: #5a6268; }
        .form-group { margin: 10px 0; }
        .form-group label { display: block; margin-bottom: 5px; font-weight: bold; }
        .form-group input, .form-group select { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; box-sizing: border-box; }
        .projects-list { border: 1px solid #ddd; border-radius: 5px; max-height: 500px; overflow-y: auto; }
        .project-item { padding: 15px; border-bottom: 1px solid #eee; display: flex; align-items: center; justify-content: space-between; }
        .project-item:hover { background: #f8f9fa; }
        .project-item:last-child { border-bottom: none; }
        .project-info { flex: 1; }
        .project-name { font-weight: bold; color: #333; margin-bottom: 5px; }
        .project-path { font-size: 0.9em; color: #666; }
        .project-actions { display: flex; gap: 8px; flex-wrap: wrap; }
        .btn-sm { padding: 8px 12px; font-size: 0.85em; }
        .loading-text { text-align: center; padding: 20px; color: #666; }
        .modal { display: none; position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.5); z-index: 1000; }
        .modal-content { position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); background: white; padding: 30px; border-radius: 10px; min-width: 400px; }
        .modal-header { margin-bottom: 20px; }
        .modal-footer { margin-top: 20px; text-align: right; }
        .output { background: #f8f9fa; padding: 15px; border-radius: 5px; font-family: monospace; white-space: pre-wrap; max-height: 300px; overflow-y: auto; }
        .status { padding: 10px; border-radius: 5px; margin: 10px 0; }
        .status.success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .status.error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🚀 SSH GitHub Manager</h1>
            <div class="config-info">
                <strong>📡 Server:</strong> {{.Host}} | 
                <strong>👤 User:</strong> {{.User}} | 
                <strong>🔐 Auth:</strong> {{.AuthMethod}} | 
                <strong>📁 Dir:</strong> {{.WorkingDir}} |
                <strong>🐙 Token:</strong> {{if .GitHubToken}}✅ Available{{else}}❌ Missing{{end}}
                <br>
                <button class="btn btn-secondary" onclick="window.location.href='/setup'">⚙️ Settings</button>
                {{if not .GitHubToken}}
                <span style="color: #dc3545; font-weight: bold;">⚠️ GitHub Token required!</span>
                {{end}}
            </div>
        </div>

        <div class="section">
            <h3>📁 Projects</h3>
            <div class="projects-list" id="projectsList">
                <div class="loading-text">Loading...</div>
            </div>
            <button class="btn" onclick="refreshProjects()">🔄 Refresh</button>
        </div>

        <div class="section">
            <h3>📥 Clone Repository</h3>
            <div class="form-group">
                <label>Repository URL:</label>
                <input type="text" id="repoUrl" placeholder="https://github.com/username/repository.git">
            </div>
            <div class="form-group">
                <label>Branch (optional):</label>
                <input type="text" id="branch" placeholder="main, master, develop...">
            </div>
            <button class="btn btn-success" onclick="gitClone()">📥 Clone Repository</button>
        </div>

        <div class="section">
            <h3>📝 Output</h3>
            <div class="output" id="output">Operation results will be shown here...</div>
        </div>
    </div>

    <!-- Commit Modal -->
    <div id="commitModal" class="modal">
        <div class="modal-content">
            <div class="modal-header">
                <h3>💾 Commit Message</h3>
            </div>
            <div class="form-group">
                <label>Commit Message:</label>
                <input type="text" id="modalCommitMessage" placeholder="Update files" value="Update files">
            </div>
            <div class="modal-footer">
                <button class="btn btn-secondary" onclick="closeCommitModal()">❌ Cancel</button>
                <button class="btn btn-success" onclick="confirmPush()">✅ Commit & Push</button>
            </div>
        </div>
    </div>

    <script>
        var currentPushPath = '';

        function showOutput(text, isError) {
            var output = document.getElementById('output');
            if (output) {
                output.textContent = text;
                output.className = 'output ' + (isError ? 'error' : 'success');
            } else {
                alert(text);
            }
        }

        function refreshProjects() {
            var projectsList = document.getElementById('projectsList');
            if (!projectsList) return;
            
            projectsList.innerHTML = '<div class="loading-text">Loading...</div>';
            
            fetch('/projects')
                .then(function(response) { return response.json(); })
                .then(function(data) {
                    if (data.error) {
                        projectsList.innerHTML = '<div class="loading-text">❌ ' + data.error + '</div>';
                        return;
                    }
                    displayProjects(data.projects || []);
                })
                .catch(function(error) {
                    projectsList.innerHTML = '<div class="loading-text">❌ Error: ' + error.message + '</div>';
                });
        }

        function displayProjects(projects) {
            var projectsList = document.getElementById('projectsList');
            if (!projectsList) return;
            
            if (projects.length === 0) {
                projectsList.innerHTML = '<div class="loading-text">📁 No Git repositories found</div>';
                return;
            }
            
            projectsList.innerHTML = '';
            
            for (var i = 0; i < projects.length; i++) {
                var project = projects[i];
                var item = document.createElement('div');
                item.className = 'project-item';
                
                var info = document.createElement('div');
                info.className = 'project-info';
                
                var name = document.createElement('div');
                name.className = 'project-name';
                name.textContent = '📁 ' + project.name;
                
                var path = document.createElement('div');
                path.className = 'project-path';
                path.textContent = project.path;
                
                info.appendChild(name);
                info.appendChild(path);
                
                var actions = document.createElement('div');
                actions.className = 'project-actions';
                
                var pullBtn = document.createElement('button');
                pullBtn.className = 'btn btn-warning btn-sm';
                pullBtn.textContent = '⬇️ Pull';
                pullBtn.onclick = (function(projectPath) {
                    return function() { gitPull(projectPath); };
                })(project.path);
                
                var pushBtn = document.createElement('button');
                pushBtn.className = 'btn btn-success btn-sm';
                pushBtn.textContent = '⬆️ Push';
                pushBtn.onclick = (function(projectPath) {
                    return function() { openCommitModal(projectPath); };
                })(project.path);

                var statusBtn = document.createElement('button');
                statusBtn.className = 'btn btn-secondary btn-sm';
                statusBtn.textContent = '📊 Status';
                statusBtn.onclick = (function(projectPath) {
                    return function() { gitStatus(projectPath); };
                })(project.path);

                var removeBtn = document.createElement('button');
                removeBtn.className = 'btn btn-danger btn-sm';
                removeBtn.textContent = '🗑️ Remove';
                removeBtn.onclick = (function(projectPath, projectName) {
                    return function() { 
                        if (confirm('Are you sure you want to delete this project?\\n\\n' + projectName + '\\n' + projectPath)) {
                            removeProject(projectPath);
                        }
                    };
                })(project.path, project.name);
                
                actions.appendChild(pullBtn);
                actions.appendChild(pushBtn);
                actions.appendChild(statusBtn);
                actions.appendChild(removeBtn);
                
                item.appendChild(info);
                item.appendChild(actions);
                projectsList.appendChild(item);
            }
        }

        function gitClone() {
            var repoUrlInput = document.getElementById('repoUrl');
            var branchInput = document.getElementById('branch');
            
            if (!repoUrlInput) {
                showOutput('Repository URL input not found!', true);
                return;
            }
            
            var repoUrl = repoUrlInput.value.trim();
            var branch = branchInput ? branchInput.value.trim() : '';
            
            if (!repoUrl) {
                showOutput('Please enter Repository URL!', true);
                return;
            }

            showOutput('🔄 Cloning...');
            
            fetch('/git/clone', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({repo_url: repoUrl, branch: branch})
            })
            .then(function(response) { return response.text(); })
            .then(function(result) {
                showOutput(result);
                // Clear inputs on successful clone
                repoUrlInput.value = '';
                if (branchInput) branchInput.value = '';
                // Refresh projects
                refreshProjects();
            })
            .catch(function(error) { 
                showOutput('❌ Clone error: ' + error.message, true); 
            });
        }

        function gitPull(projectPath) {
            showOutput('🔄 Pulling: ' + projectPath);
            
            fetch('/git/pull', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({repo_path: projectPath})
            })
            .then(function(response) { return response.text(); })
            .then(function(result) {
                showOutput(result);
            })
            .catch(function(error) { 
                showOutput('❌ Pull error: ' + error.message, true); 
            });
        }

        function openCommitModal(projectPath) {
            currentPushPath = projectPath;
            var modal = document.getElementById('commitModal');
            var messageInput = document.getElementById('modalCommitMessage');
            
            if (modal && messageInput) {
                messageInput.value = 'Update files';
                modal.style.display = 'block';
                messageInput.focus();
                messageInput.select();
            }
        }

        function closeCommitModal() {
            var modal = document.getElementById('commitModal');
            if (modal) {
                modal.style.display = 'none';
            }
            currentPushPath = '';
        }

        function confirmPush() {
            var messageInput = document.getElementById('modalCommitMessage');
            var message = messageInput ? messageInput.value.trim() : 'Update files';
            
            closeCommitModal();
            
            if (!currentPushPath) {
                showOutput('❌ Push path unknown!', true);
                return;
            }
            
            showOutput('🔄 Pushing: ' + currentPushPath);
            
            fetch('/git/push', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({repo_path: currentPushPath, message: message})
            })
            .then(function(response) { return response.text(); })
            .then(function(result) {
                showOutput(result);
            })
            .catch(function(error) { 
                showOutput('❌ Push error: ' + error.message, true); 
            });
        }

        function gitStatus(projectPath) {
            showOutput('🔄 Checking status: ' + projectPath);
            
            fetch('/git/status', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({repo_path: projectPath})
            })
            .then(function(response) { return response.text(); })
            .then(function(result) { 
                showOutput(result); 
            })
            .catch(function(error) { 
                showOutput('❌ Status error: ' + error.message, true); 
            });
        }

        function removeProject(projectPath) {
            showOutput('🔄 Removing project: ' + projectPath);
            
            fetch('/git/remove', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({repo_path: projectPath})
            })
            .then(function(response) { return response.text(); })
            .then(function(result) {
                showOutput(result);
                refreshProjects();
            })
            .catch(function(error) { 
                showOutput('❌ Remove error: ' + error.message, true); 
            });
        }

        // Close modal with ESC key
        document.addEventListener('keydown', function(event) {
            if (event.key === 'Escape') {
                closeCommitModal();
            }
        });

        // Close modal by clicking background
        var commitModal = document.getElementById('commitModal');
        if (commitModal) {
            commitModal.addEventListener('click', function(event) {
                if (event.target === this) {
                    closeCommitModal();
                }
            });
        }

        // Commit with Enter key
        document.addEventListener('keydown', function(event) {
            if (event.key === 'Enter' && document.getElementById('commitModal').style.display === 'block') {
                confirmPush();
            }
        });

        // Load projects on page load
        window.onload = function() {
            refreshProjects();
        };
    </script>
</body>
</html>`

	t := template.Must(template.New("index").Parse(tmpl))
	data := struct {
		Host        string
		User        string
		AuthMethod  string
		WorkingDir  string
		GitHubToken string
	}{
		Host:        config.SSHHost,
		User:        config.SSHUser,
		AuthMethod:  config.AuthMethod,
		WorkingDir:  config.WorkingDir,
		GitHubToken: config.GitHubToken,
	}

	t.Execute(w, data)
}

func setupHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := `<!DOCTYPE html>
<html>
<head>
    <title>SSH GitHub Manager - Setup</title>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background: #f5f5f5; }
        .container { max-width: 800px; margin: 0 auto; background: white; padding: 30px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .header { text-align: center; margin-bottom: 30px; color: #333; }
        .form-group { margin: 15px 0; }
        .form-group label { display: block; margin-bottom: 5px; font-weight: bold; }
        .form-group input, .form-group select { width: 100%; padding: 10px; border: 1px solid #ddd; border-radius: 4px; font-size: 14px; box-sizing: border-box; }
        .form-group input:focus, .form-group select:focus { outline: none; border-color: #007bff; }
        .btn { padding: 12px 24px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; margin: 5px; }
        .btn:hover { background: #0056b3; }
        .btn-success { background: #28a745; }
        .btn-success:hover { background: #1e7e34; }
        .btn-secondary { background: #6c757d; }
        .btn-secondary:hover { background: #5a6268; }
        .auth-section { display: none; padding: 15px; background: #f8f9fa; border-radius: 5px; margin: 10px 0; }
        .auth-section.active { display: block; }
        .status { padding: 15px; border-radius: 5px; margin: 15px 0; }
        .status.success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .status.error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
        .status.info { background: #d1ecf1; color: #0c5460; border: 1px solid #bee5eb; }
        .help-text { font-size: 12px; color: #666; margin-top: 5px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🔧 SSH GitHub Manager Setup</h1>
            <p>Configure your server connection settings</p>
        </div>

        <form id="configForm">
            <div class="form-group">
                <label>🌐 Server Host/IP:</label>
                <input type="text" id="sshHost" name="ssh_host" value="{{.SSHHost}}" placeholder="192.168.1.100 or example.com" required>
                <div class="help-text">Your server IP address or domain name</div>
            </div>

            <div class="form-group">
                <label>🔌 SSH Port:</label>
                <input type="number" id="sshPort" name="ssh_port" value="{{.SSHPort}}" placeholder="22" required>
                <div class="help-text">SSH port (usually 22)</div>
            </div>

            <div class="form-group">
                <label>👤 SSH User:</label>
                <input type="text" id="sshUser" name="ssh_user" value="{{.SSHUser}}" placeholder="root or username" required>
                <div class="help-text">Server username</div>
            </div>

            <div class="form-group">
                <label>🔐 Authentication Method:</label>
                <select id="authMethod" name="auth_method" onchange="toggleAuthMethod()" required>
                    <option value="password"{{if eq .AuthMethod "password"}} selected{{end}}>Password</option>
                    <option value="key"{{if eq .AuthMethod "key"}} selected{{end}}>SSH Key</option>
                </select>
            </div>

            <div id="passwordAuth" class="auth-section">
                <div class="form-group">
                    <label>🔑 SSH Password:</label>
                    <input type="password" id="sshPassword" name="ssh_password" value="{{.SSHPassword}}" placeholder="Your server password">
                    <div class="help-text">Password for SSH connection</div>
                </div>
            </div>

            <div id="keyAuth" class="auth-section">
                <div class="form-group">
                    <label>🗝️ SSH Key Path:</label>
                    <input type="text" id="sshKeyPath" name="ssh_key_path" value="{{.SSHKeyPath}}" placeholder="/home/username/.ssh/id_rsa">
                    <div class="help-text">Full path to SSH private key file</div>
                </div>
            </div>

            <div class="form-group">
                <label>📁 Working Directory:</label>
                <input type="text" id="workingDir" name="working_dir" value="{{.WorkingDir}}" placeholder="/root/projects" required>
                <div class="help-text">Directory on server where Git repositories will be stored</div>
            </div>

            <div class="form-group">
                <label>🐙 GitHub Token (Required!):</label>
                <input type="password" id="githubToken" name="github_token" value="{{.GitHubToken}}" placeholder="ghp_xxxxxxxxxxxx" required>
                <div class="help-text">GitHub Personal Access Token is required for repositories. <a href="https://github.com/settings/tokens" target="_blank">Create one here</a></div>
            </div>

            <div style="text-align: center; margin-top: 30px;">
                <button type="button" class="btn btn-secondary" onclick="testConnection()">🔍 Test Connection</button>
                <button type="submit" class="btn btn-success">💾 Save Settings</button>
            </div>
        </form>

        <div id="status"></div>

        {{if .IsConfigured}}
        <div style="text-align: center; margin-top: 20px;">
            <button class="btn" onclick="window.location.href='/'">🏠 Back to Home</button>
        </div>
        {{end}}
    </div>

    <script>
        function toggleAuthMethod() {
            var authMethod = document.getElementById('authMethod').value;
            var passwordAuth = document.getElementById('passwordAuth');
            var keyAuth = document.getElementById('keyAuth');
            
            if (authMethod === 'password') {
                passwordAuth.classList.add('active');
                keyAuth.classList.remove('active');
            } else {
                passwordAuth.classList.remove('active');
                keyAuth.classList.add('active');
            }
        }

        function showStatus(message, type) {
            var status = document.getElementById('status');
            status.innerHTML = '<div class="status ' + type + '">' + message + '</div>';
        }

        function testConnection() {
            var formData = new FormData(document.getElementById('configForm'));
            var config = {};
            for (var pair of formData.entries()) {
                config[pair[0]] = pair[1];
            }
            
            showStatus('🔄 Testing connection...', 'info');
            
            fetch('/test-connection', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(config)
            })
            .then(function(response) { return response.json(); })
            .then(function(result) {
                if (result.success) {
                    showStatus('✅ Connection successful! Server: ' + result.message, 'success');
                } else {
                    showStatus('❌ Connection error: ' + result.error, 'error');
                }
            })
            .catch(function(error) {
                showStatus('❌ Test error: ' + error.message, 'error');
            });
        }

        document.getElementById('configForm').addEventListener('submit', function(e) {
            e.preventDefault();
            
            var formData = new FormData(this);
            var config = {};
            for (var pair of formData.entries()) {
                config[pair[0]] = pair[1];
            }
            
            showStatus('💾 Saving settings...', 'info');
            
            fetch('/save-config', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(config)
            })
            .then(function(response) { return response.json(); })
            .then(function(result) {
                if (result.success) {
                    showStatus('✅ Settings saved successfully! Redirecting in 2 seconds...', 'success');
                    setTimeout(function() {
                        window.location.href = '/';
                    }, 2000);
                } else {
                    showStatus('❌ Save error: ' + result.error, 'error');
                }
            })
            .catch(function(error) {
                showStatus('❌ Error: ' + error.message, 'error');
            });
        });

        // Show auth method on page load
        window.onload = function() {
            toggleAuthMethod();
        };
    </script>
</body>
</html>`

	t := template.Must(template.New("setup").Parse(tmpl))
	t.Execute(w, config)
}

func projectsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check SSH connection
	if sshManager.client == nil {
		if err := sshManager.Connect(); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":    "SSH connection not established: " + err.Error(),
				"projects": []Project{},
			})
			return
		}
	}

	projects, err := sshManager.ListProjects()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":    "Failed to get project list: " + err.Error(),
			"projects": []Project{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"projects": projects,
		"error":    nil,
	})
}

func gitCloneHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("🌐 Clone request received")

	if r.Method != "POST" {
		log.Printf("❌ Wrong method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SSH connection
	if sshManager.client == nil {
		log.Printf("🔌 SSH reconnecting")
		if err := sshManager.Connect(); err != nil {
			log.Printf("❌ SSH connection error: %v", err)
			fmt.Fprintf(w, "❌ SSH connection error: %v", err)
			return
		}
	}

	var req struct {
		RepoURL string `json:"repo_url"`
		Branch  string `json:"branch"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ JSON decode error: %v", err)
		fmt.Fprintf(w, "❌ JSON parse error: %v", err)
		return
	}

	log.Printf("📥 Clone request: %s (branch: %s)", req.RepoURL, req.Branch)
	result, err := sshManager.GitClone(req.RepoURL, req.Branch)
	if err != nil {
		log.Printf("❌ Clone failed")
		fmt.Fprintf(w, "❌ Clone error: %v\n%s", err, result)
		return
	}

	log.Printf("✅ Clone successful")
	fmt.Fprintf(w, "✅ Clone completed successfully!\n%s", result)
}

func gitPullHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("🌐 Pull request received")

	if r.Method != "POST" {
		log.Printf("❌ Wrong method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SSH connection
	if sshManager.client == nil {
		log.Printf("🔌 SSH reconnecting")
		if err := sshManager.Connect(); err != nil {
			log.Printf("❌ SSH connection error: %v", err)
			fmt.Fprintf(w, "❌ SSH connection error: %v", err)
			return
		}
	}

	var req struct {
		RepoPath string `json:"repo_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ JSON decode error: %v", err)
		fmt.Fprintf(w, "❌ JSON parse error: %v", err)
		return
	}

	log.Printf("⬇️ Pull request: %s", req.RepoPath)
	result, err := sshManager.GitPull(req.RepoPath)
	if err != nil {
		log.Printf("❌ Pull failed")
		fmt.Fprintf(w, "❌ Pull error: %v\n%s", err, result)
		return
	}

	log.Printf("✅ Pull successful")
	fmt.Fprintf(w, "✅ Pull completed successfully!\n%s", result)
}

func gitPushHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("🌐 Push request received")

	if r.Method != "POST" {
		log.Printf("❌ Wrong method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SSH connection
	if sshManager.client == nil {
		log.Printf("🔌 SSH reconnecting")
		if err := sshManager.Connect(); err != nil {
			log.Printf("❌ SSH connection error: %v", err)
			fmt.Fprintf(w, "❌ SSH connection error: %v", err)
			return
		}
	}

	var req struct {
		RepoPath string `json:"repo_path"`
		Message  string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ JSON decode error: %v", err)
		fmt.Fprintf(w, "❌ JSON parse error: %v", err)
		return
	}

	log.Printf("⬆️ Push request: %s (message: %s)", req.RepoPath, req.Message)
	result, err := sshManager.GitPush(req.RepoPath, req.Message)
	if err != nil {
		log.Printf("❌ Push failed")
		fmt.Fprintf(w, "❌ Push error: %v\n%s", err, result)
		return
	}

	log.Printf("✅ Push successful")
	fmt.Fprintf(w, "✅ Push completed successfully!\n%s", result)
}

func gitStatusHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("🌐 Status request received")

	if r.Method != "POST" {
		log.Printf("❌ Wrong method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SSH connection
	if sshManager.client == nil {
		log.Printf("🔌 SSH reconnecting")
		if err := sshManager.Connect(); err != nil {
			log.Printf("❌ SSH connection error: %v", err)
			fmt.Fprintf(w, "❌ SSH connection error: %v", err)
			return
		}
	}

	var req struct {
		RepoPath string `json:"repo_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ JSON decode error: %v", err)
		fmt.Fprintf(w, "❌ JSON parse error: %v", err)
		return
	}

	log.Printf("📊 Status request: %s", req.RepoPath)
	result, err := sshManager.GitStatus(req.RepoPath)
	if err != nil {
		log.Printf("❌ Status failed")
		fmt.Fprintf(w, "❌ Status error: %v\n%s", err, result)
		return
	}

	log.Printf("✅ Status successful")
	fmt.Fprintf(w, "📊 Repository Status:\n%s", result)
}

func gitRemoveHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("🌐 Remove request received")

	if r.Method != "POST" {
		log.Printf("❌ Wrong method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SSH connection
	if sshManager.client == nil {
		log.Printf("🔌 SSH reconnecting")
		if err := sshManager.Connect(); err != nil {
			log.Printf("❌ SSH connection error: %v", err)
			fmt.Fprintf(w, "❌ SSH connection error: %v", err)
			return
		}
	}

	var req struct {
		RepoPath string `json:"repo_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ JSON decode error: %v", err)
		fmt.Fprintf(w, "❌ JSON parse error: %v", err)
		return
	}

	log.Printf("🗑️ Remove request: %s", req.RepoPath)
	result, err := sshManager.RemoveProject(req.RepoPath)
	if err != nil {
		log.Printf("❌ Remove failed")
		fmt.Fprintf(w, "❌ Remove error: %v\n%s", err, result)
		return
	}

	log.Printf("✅ Remove successful")
	fmt.Fprintf(w, "✅ Project deleted successfully!\n%s", result)
}

func saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newConfig Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "JSON parse error: " + err.Error(),
		})
		return
	}

	// Update configuration
	newConfig.IsConfigured = true
	config = &newConfig

	// Recreate SSH manager
	sshManager.Disconnect()
	sshManager = NewSSHManager(config)

	// Save to file
	if err := saveConfig(config); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Configuration not saved: " + err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Configuration saved successfully",
	})
}

func testConnectionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var testConfig Config
	if err := json.NewDecoder(r.Body).Decode(&testConfig); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "JSON parse error: " + err.Error(),
		})
		return
	}

	// Create temporary SSH manager for testing
	testManager := NewSSHManager(&testConfig)

	if err := testManager.Connect(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Run test command
	output, err := testManager.ExecuteCommand("hostname && pwd")
	testManager.Disconnect()

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Command execution error: " + err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": strings.TrimSpace(output),
	})
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}
