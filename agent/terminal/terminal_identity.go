package terminal

import "os/user"

func currentUsername() string {
	if current, err := user.Current(); err == nil {
		return current.Username
	}
	return ""
}
