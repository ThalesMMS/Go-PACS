//go:build !cgo

package main

import (
	"os/exec"
	"runtime"
)

func runUI(url string, serverDone <-chan error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
	<-serverDone
}
