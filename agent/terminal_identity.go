package agent

import "os/user"

// currentUsername returns the OS user name of the agent process.
func currentUsername() string {
	if current, err := user.Current(); err == nil {
		return current.Username
	}
	return ""
}
