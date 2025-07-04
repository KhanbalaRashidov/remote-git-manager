# SSH GitHub Manager

A simple web-based tool to manage Git repositories on remote servers via SSH.

## Features

- **Web Interface**: Manage repositories from your browser
- **SSH Connection**: Connect to remote servers securely
- **Git Operations**: Clone, pull, push, status, and remove repositories
- **GitHub Integration**: Works with GitHub Personal Access Tokens
- **Real-time Output**: See command results instantly

## Installation

1. Clone this repository:
```bash
git clone https://github.com/yourusername/ssh-github-manager.git
cd ssh-github-manager
```

2. Install dependencies:
```bash
go mod init ssh-github-manager
go get golang.org/x/crypto/ssh
```

3. Run the application:
```bash
go run main.go
```

4. Open your browser and go to `http://localhost:8080`

## Setup

1. **Server Settings**:
    - Enter your server IP or hostname
    - Set SSH port (usually 22)
    - Enter your username
    - Choose authentication method (password or SSH key)
    - Set working directory where repositories will be stored

2. **GitHub Token**:
    - Go to [GitHub Settings > Tokens](https://github.com/settings/tokens)
    - Create new token with `repo` permission
    - Enter the token in setup form

3. **Test Connection** and save your settings

## Usage

### Clone Repository
1. Enter repository URL: `https://github.com/username/repository.git`
2. Optionally enter branch name
3. Click "Clone Repository"

### Manage Projects
Each project in the list has these buttons:
- **Pull**: Get latest changes from remote
- **Push**: Commit and push your changes (opens dialog for commit message)
- **Status**: Check repository status
- **Remove**: Delete project from server (asks for confirmation)

## Configuration

The app creates a `config.json` file with your settings:

```json
{
  "ssh_host": "your-server.com",
  "ssh_port": "22",
  "ssh_user": "username",
  "auth_method": "password",
  "ssh_password": "your-password",
  "working_dir": "/home/user/projects",
  "github_token": "ghp_your_token_here",
  "is_configured": true
}
```

## Requirements

- Go 1.24 or higher
- SSH access to your remote server
- Git installed on the remote server
- GitHub Personal Access Token (for GitHub repositories)

## Troubleshooting

**SSH Connection Issues**:
- Check if you can connect manually: `ssh user@server`
- Verify SSH key permissions: `chmod 600 ~/.ssh/id_rsa`
- Make sure SSH service is running on the server

**GitHub Authentication**:
- Check if your token has correct permissions
- Verify token hasn't expired
- Make sure repository URL is correct

**Repository Not Found**:
- Check if working directory exists on server
- Verify Git is installed: `git --version`
- Make sure you have write permissions

## License

MIT License - see LICENSE file for details.